// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package asctrust

import (
	"strings"
	"testing"
	"time"

	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/test"
	"forgejo.org/services/ascidentity"
	files_service "forgejo.org/services/repository/files"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
)

// The edit of the policy file through the editor or the contents API is proposed as the person, and asc says whether
// that person may write the file.
func policyEditFake(t *testing.T, mayWrite bool) *fakeAsc {
	t.Helper()
	f, _ := startFake(t)
	f.getSource = func(*ascv1.GetPolicySourceRequest) (*ascv1.GetPolicySourceResponse, error) {
		return policySource(), nil
	}
	f.evaluate = func(req *ascv1.EvaluatePolicyRequest) (*ascv1.EvaluatePolicyResponse, error) {
		d := &ascv1.PolicyDecision{Path: req.GetPaths()[0], Allowed: mayWrite}
		if !mayWrite {
			d.Reason = "the repository policy can be changed only by a maintainer"
		}
		return &ascv1.EvaluatePolicyResponse{Decisions: []*ascv1.PolicyDecision{d}}, nil
	}
	change := make([]byte, 16)
	change[0] = 0x21
	f.propose = func(*ascv1.ProposePolicyRequest) (*ascv1.ProposePolicyResponse, error) {
		return &ascv1.ProposePolicyResponse{Change: change, Changeset: make([]byte, 32)}, nil
	}
	f.gitMap = func(*ascv1.GitMapRequest) (*ascv1.GitMapResponse, error) { return &ascv1.GitMapResponse{}, nil }
	return f
}

// claimOf verifies the claim a call arrived with, as the server would.
func claimOf(c fakeCall, subject string) (user, role string, err error) {
	return ascidentity.Verify(testForwardedKey, func(k string) string { return strings.Join(c.MD.Get(k), ",") }, subject, time.Now())
}

func useNoForwardedKey(t *testing.T) {
	t.Helper()
	t.Cleanup(test.MockVariableValue(&setting.Asc.ForwardedKey, []byte(nil)))
}

func policyEdit(path, text string) *files_service.ChangeRepoFilesOptions {
	return &files_service.ChangeRepoFilesOptions{
		OldBranch: "master", NewBranch: "master", Message: "policy: tighten",
		Files: []*files_service.ChangeRepoFile{{Operation: "update", TreePath: path, ContentReader: strings.NewReader(text)}},
	}
}

func TestPolicyEditByAMaintainerIsProposedAsThePerson(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	useForwardedKey(t)
	repo := ascRepo(t)
	require.Equal(t, "master", repo.DefaultBranch)
	maintainer := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2})
	f := policyEditFake(t, true)

	err := proposePolicyEdit(t.Context(), repo, maintainer, policyEdit(".asc/policy.toml", strings.ReplaceAll(boardPolicyText, "\n", "\r\n")+"\n# tighter\n"))
	var proposed *files_service.ErrAscPolicyProposed
	require.ErrorAs(t, err, &proposed, "the edit is proposed and never committed")
	assert.Len(t, proposed.Change, 12)
	assert.Empty(t, proposed.Link, "the git view has no commit for it yet")

	// asc was asked as the person, for the one file, before anything was proposed.
	eval := f.last(t, "Evaluate")
	user, role, cerr := claimOf(eval, "user2/repo1")
	require.NoError(t, cerr)
	assert.Equal(t, "2:user2", user)
	assert.Contains(t, []string{"write", "admin"}, role, "the claim is what the hub decided for this person")
	assert.Equal(t, ascv1.PolicyAction_POLICY_ACTION_WRITE, eval.Req.(*ascv1.EvaluatePolicyRequest).GetAction())
	assert.Equal(t, [][]byte{[]byte(".asc/policy.toml")}, eval.Req.(*ascv1.EvaluatePolicyRequest).GetPaths())

	prop := f.last(t, "Propose")
	req := prop.Req.(*ascv1.ProposePolicyRequest)
	assert.NotContains(t, string(req.GetText()), "\r", "carriage returns of the form are dropped")
	assert.True(t, strings.HasSuffix(string(req.GetText()), "# tighter\n"))
	assert.Equal(t, "policy: tighten", string(req.GetMessage()))
	assert.Equal(t, policySource().GetSource(), req.GetBase(), "the proposal starts from the trunk that was read")
	user, _, cerr = claimOf(prop, "user2/repo1")
	require.NoError(t, cerr)
	assert.Equal(t, "2:user2", user, "the proposal is made as the person, not as the hub")
}

func TestPolicyEditByAnyoneAscRefusesIsRefusedWithItsReason(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	useForwardedKey(t)
	repo := ascRepo(t)
	writer := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2}) // may write the repository, is no maintainer
	f := policyEditFake(t, false)

	err := proposePolicyEdit(t.Context(), repo, writer, policyEdit(".asc/policy.toml", boardPolicyText+"\n# me\n"))
	var refused *files_service.ErrAscPolicyRefused
	require.ErrorAs(t, err, &refused)
	assert.Contains(t, refused.Reason, "only by a maintainer")
	assert.Contains(t, refused.Reason, "Ask a maintainer")
	assert.Zero(t, f.count("Propose"), "nothing is proposed for a person asc refuses")
}

func TestPolicyEditLeavesEverythingElseAlone(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	useForwardedKey(t)
	repo := ascRepo(t)
	person := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2})
	f := policyEditFake(t, true)

	t.Run("another file", func(t *testing.T) {
		assert.NoError(t, proposePolicyEdit(t.Context(), repo, person, policyEdit("README.md", "x")))
		assert.NoError(t, proposePolicyEdit(t.Context(), repo, person, policyEdit(".asc/workflows/review.toml", "x")))
	})
	t.Run("another branch", func(t *testing.T) {
		opts := policyEdit(".asc/policy.toml", boardPolicyText+"\n# x\n")
		opts.OldBranch, opts.NewBranch = "feature", "feature"
		assert.NoError(t, proposePolicyEdit(t.Context(), repo, person, opts))
	})
	t.Run("a repository with no usable policy", func(t *testing.T) {
		f.getSource = func(*ascv1.GetPolicySourceRequest) (*ascv1.GetPolicySourceResponse, error) {
			return &ascv1.GetPolicySourceResponse{Present: false}, nil
		}
		assert.NoError(t, proposePolicyEdit(t.Context(), repo, person, policyEdit(".asc/policy.toml", boardPolicyText)), "the first file is written under the gate's own rules")
		f.getSource = func(*ascv1.GetPolicySourceRequest) (*ascv1.GetPolicySourceResponse, error) {
			src := policySource()
			src.Parsed = &ascv1.GetPolicyResponse{Error: "line 3: unknown key"}
			return src, nil
		}
		assert.NoError(t, proposePolicyEdit(t.Context(), repo, person, policyEdit(".asc/policy.toml", boardPolicyText)), "a broken file is repaired under the gate's own rules")
		assert.Zero(t, f.count("Propose"))
	})
	t.Run("a repository that is not in asc", func(t *testing.T) {
		git := unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{ID: 2})
		assert.NoError(t, proposePolicyEdit(t.Context(), git, person, policyEdit(".asc/policy.toml", boardPolicyText)))
	})
}

func TestPolicyEditRefusesWhatCannotBeProposed(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	useForwardedKey(t)
	repo := ascRepo(t)
	person := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2})
	f := policyEditFake(t, true)

	refusal := func(t *testing.T, opts *files_service.ChangeRepoFilesOptions) string {
		t.Helper()
		var refused *files_service.ErrAscPolicyRefused
		require.ErrorAs(t, proposePolicyEdit(t.Context(), repo, person, opts), &refused)
		return refused.Reason
	}
	good := func() *files_service.ChangeRepoFilesOptions {
		return policyEdit(".asc/policy.toml", boardPolicyText+"\n# x\n")
	}

	t.Run("a delete", func(t *testing.T) {
		opts := policyEdit(".asc/policy.toml", "")
		opts.Files[0].Operation = "delete"
		assert.Contains(t, refusal(t, opts), "not deleted")
	})
	t.Run("a rename", func(t *testing.T) {
		opts := policyEdit(".asc/policy.toml", boardPolicyText+"\n# x\n")
		opts.Files[0].FromTreePath = "policy.toml"
		assert.NotEmpty(t, refusal(t, opts))
		opts = policyEdit("docs/policy.toml", "x")
		opts.Files[0].FromTreePath = ".asc/policy.toml"
		assert.NotEmpty(t, refusal(t, opts))
	})
	t.Run("another spelling of the name", func(t *testing.T) {
		assert.Contains(t, refusal(t, policyEdit(".ASC/Policy.toml", boardPolicyText+"\n# x\n")), "own name")
	})
	t.Run("together with another file", func(t *testing.T) {
		opts := policyEdit(".asc/policy.toml", boardPolicyText+"\n# x\n")
		opts.Files = append(opts.Files, &files_service.ChangeRepoFile{Operation: "create", TreePath: "a.txt", ContentReader: strings.NewReader("a")})
		assert.Contains(t, refusal(t, opts), "on its own")
	})
	t.Run("the same text", func(t *testing.T) {
		assert.Contains(t, refusal(t, policyEdit(".asc/policy.toml", boardPolicyText)), "Nothing changed")
	})
	t.Run("a file asc rejects", func(t *testing.T) {
		before := f.count("Propose")
		assert.Contains(t, refusal(t, policyEdit(".asc/policy.toml", strings.Repeat("x", MaxPolicyBytes+1))), "file")
		assert.Equal(t, before, f.count("Propose"))
	})
	t.Run("a lockout", func(t *testing.T) {
		f.propose = func(*ascv1.ProposePolicyRequest) (*ascv1.ProposePolicyResponse, error) {
			return nil, detailedError(t, codes.FailedPrecondition, &ascv1.ErrorDetail{Code: "POLICY_LOCKOUT"})
		}
		assert.Contains(t, refusal(t, good()), "lock you out")
	})
	t.Run("a trunk that moved", func(t *testing.T) {
		f.propose = func(*ascv1.ProposePolicyRequest) (*ascv1.ProposePolicyResponse, error) {
			return nil, detailedError(t, codes.Aborted, &ascv1.ErrorDetail{Code: "VERSION_CONFLICT"})
		}
		assert.Contains(t, refusal(t, good()), "changed while you were editing")
	})
	t.Run("an answer that is not about the file", func(t *testing.T) {
		f.evaluate = func(*ascv1.EvaluatePolicyRequest) (*ascv1.EvaluatePolicyResponse, error) {
			return &ascv1.EvaluatePolicyResponse{Decisions: []*ascv1.PolicyDecision{{Path: []byte("other"), Allowed: true}}}, nil
		}
		before := f.count("Propose")
		assert.NotEmpty(t, refusal(t, good()))
		assert.Equal(t, before, f.count("Propose"), "an unverified yes is a no")
	})
	t.Run("a person asc cannot be asked for", func(t *testing.T) {
		// No key to sign the claim: the hub does not fall back to its own credential.
		useNoForwardedKey(t)
		assert.NotEmpty(t, refusal(t, good()))
	})
}
