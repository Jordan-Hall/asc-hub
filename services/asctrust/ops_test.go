// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package asctrust

import (
	"context"
	"strings"
	"testing"

	repo_model "forgejo.org/models/repo"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/test"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// stubConn is a connection on which every call fails with err.
type stubConn struct{ err error }

func (s stubConn) Invoke(context.Context, string, any, any, ...grpc.CallOption) error { return s.err }

func (s stubConn) NewStream(context.Context, *grpc.StreamDesc, string, ...grpc.CallOption) (grpc.ClientStream, error) {
	return nil, s.err
}

func policySource() *ascv1.GetPolicySourceResponse {
	src := make([]byte, 32)
	src[0] = 0xab
	return &ascv1.GetPolicySourceResponse{Present: true, Source: src, Text: []byte(boardPolicyText), Parsed: boardPolicy()}
}

func TestLoadPolicyPageAssemblesWhatTheServerKnows(t *testing.T) {
	f, c := startFake(t)
	f.getSource = func(*ascv1.GetPolicySourceRequest) (*ascv1.GetPolicySourceResponse, error) {
		return policySource(), nil
	}
	f.evaluate = func(req *ascv1.EvaluatePolicyRequest) (*ascv1.EvaluatePolicyResponse, error) {
		switch req.GetAction() {
		case ascv1.PolicyAction_POLICY_ACTION_LAND:
			return &ascv1.EvaluatePolicyResponse{Land: &ascv1.PolicyLandRequirements{Approvals: 1, Checks: []string{"unit", "lint"}, Signed: true, MergeStrategy: "rebase"}}, nil
		case ascv1.PolicyAction_POLICY_ACTION_WRITE:
			var ds []*ascv1.PolicyDecision
			for _, p := range req.GetPaths() {
				ds = append(ds, &ascv1.PolicyDecision{Path: p, Allowed: !strings.HasPrefix(string(p), "infra/"), Reason: "limited to role:maintainer"})
			}
			return &ascv1.EvaluatePolicyResponse{Decisions: ds}, nil
		}
		return nil, status.Error(codes.InvalidArgument, "action")
	}
	f.listRules = func(*ascv1.ListSecretRulesRequest) (*ascv1.ListSecretRulesResponse, error) {
		return &ascv1.ListSecretRulesResponse{BlockOnPush: true}, nil
	}

	page, err := LoadPolicyPage(userCtx(t, "tok-admin"), c, "acme/payments")
	require.NoError(t, err)
	assert.Equal(t, policySource().Source, page.Base)
	v := page.View
	assert.True(t, v.Usable)
	assert.Equal(t, "ab0000000000", v.Source)
	assert.Contains(t, strings.Join(v.Meaning, "\n"), "Every change on trunk must be signed.")
	var you *WhoRow
	for i := range v.Table.Rows {
		if v.Table.Rows[i].Who == "You" {
			you = &v.Table.Rows[i]
		}
	}
	require.NotNil(t, you, "the answer for the caller is in the table")
	assert.Equal(t, "No", you.Cells[1].Text)
	assert.True(t, v.Protections[0].On)

	call := f.last(t, "GetSource")
	assert.Equal(t, "Bearer tok-admin", call.Auth)
	assert.Empty(t, call.Req.(*ascv1.GetPolicySourceRequest).GetRev(), "the trunk tip")
}

func TestLoadPolicyPageNeedsOnlyTheSource(t *testing.T) {
	f, c := startFake(t)
	f.getSource = func(*ascv1.GetPolicySourceRequest) (*ascv1.GetPolicySourceResponse, error) {
		return policySource(), nil
	}
	// evaluate and listRules are unimplemented: the page goes on with less.
	page, err := LoadPolicyPage(userCtx(t, "t"), c, "acme/payments")
	require.NoError(t, err)
	assert.True(t, page.View.Usable)
	assert.Contains(t, strings.Join(page.View.Meaning, "\n"), "Every change on trunk must be signed.", "signing is read from the text")
	for _, r := range page.View.Table.Rows {
		assert.NotEqual(t, "You", r.Who)
	}
}

func TestLoadPolicySummaryReadsOnlyTheSource(t *testing.T) {
	f, c := startFake(t)
	f.getSource = func(*ascv1.GetPolicySourceRequest) (*ascv1.GetPolicySourceResponse, error) {
		return policySource(), nil
	}
	sum, err := LoadPolicySummary(userCtx(t, "t"), c, "acme/payments")
	require.NoError(t, err)
	assert.True(t, sum.Present)
	assert.True(t, sum.Usable)
	require.NotEmpty(t, sum.Lines)
	assert.LessOrEqual(t, len(sum.Lines), maxSummaryLines)

	f.getSource = func(*ascv1.GetPolicySourceRequest) (*ascv1.GetPolicySourceResponse, error) {
		return &ascv1.GetPolicySourceResponse{}, nil
	}
	sum, err = LoadPolicySummary(userCtx(t, "t"), c, "acme/payments")
	require.NoError(t, err)
	assert.False(t, sum.Present)
	assert.Empty(t, sum.Lines)

	f.getSource = func(*ascv1.GetPolicySourceRequest) (*ascv1.GetPolicySourceResponse, error) {
		return nil, status.Error(codes.PermissionDenied, "no")
	}
	_, err = LoadPolicySummary(userCtx(t, "t"), c, "acme/payments")
	require.Error(t, err)
}

func TestLoadPolicyPageFailsWithoutTheSource(t *testing.T) {
	f, c := startFake(t)
	f.getSource = func(*ascv1.GetPolicySourceRequest) (*ascv1.GetPolicySourceResponse, error) {
		return nil, status.Error(codes.PermissionDenied, "not an admin of acme/payments")
	}
	_, err := LoadPolicyPage(userCtx(t, "t"), c, "acme/payments")
	require.Error(t, err)
	assert.Equal(t, KindDenied, Classify(err).Kind)

	f.getSource = func(*ascv1.GetPolicySourceRequest) (*ascv1.GetPolicySourceResponse, error) {
		r := policySource()
		r.Text = make([]byte, MaxPolicyBytes+1)
		return r, nil
	}
	_, err = LoadPolicyPage(userCtx(t, "t"), c, "acme/payments")
	assert.ErrorIs(t, err, ErrBadAnswer, "a policy larger than the parser limit")

	f.getSource = func(*ascv1.GetPolicySourceRequest) (*ascv1.GetPolicySourceResponse, error) {
		r := policySource()
		r.Source = []byte{1, 2, 3}
		return r, nil
	}
	_, err = LoadPolicyPage(userCtx(t, "t"), c, "acme/payments")
	assert.ErrorIs(t, err, ErrBadAnswer, "a changeset id of the wrong size")
}

func TestPreviewPolicy(t *testing.T) {
	f, c := startFake(t)
	f.validate = func(req *ascv1.ValidatePolicyRequest) (*ascv1.ValidatePolicyResponse, error) {
		if strings.Contains(string(req.GetText()), "bogus") {
			return &ascv1.ValidatePolicyResponse{Valid: false, Diagnostics: []*ascv1.PolicyDiagnostic{{Severity: ascv1.PolicySeverity_POLICY_SEVERITY_ERROR, Code: "unknown_key", Message: "unknown key `bogus`", Line: 2}}}, nil
		}
		return &ascv1.ValidatePolicyResponse{Valid: true, Parsed: boardPolicy(), CallerStaysMaintainer: !strings.Contains(string(req.GetText()), "carol")}, nil
	}
	ctx := userCtx(t, "tok")

	p, err := PreviewPolicy(ctx, c, "acme/payments", boardPolicyText)
	require.NoError(t, err)
	assert.True(t, p.Valid)
	assert.True(t, p.StaysMaintainer)
	assert.Contains(t, strings.Join(p.Meaning, "\n"), "Nobody can force-push trunk.")

	p, err = PreviewPolicy(ctx, c, "acme/payments", boardPolicyText+"# carol\n")
	require.NoError(t, err)
	assert.False(t, p.StaysMaintainer, "a draft that would lock the person out is flagged")

	p, err = PreviewPolicy(ctx, c, "acme/payments", "bogus = 1\n")
	require.NoError(t, err)
	assert.False(t, p.Valid)
	require.Len(t, p.Diagnostics, 1)
	assert.Equal(t, "unknown key `bogus`", p.Diagnostics[0].Message)
	assert.EqualValues(t, 2, p.Diagnostics[0].Line)
	assert.Empty(t, p.Meaning, "an invalid draft is not explained")

	n := f.count("Validate")
	for _, bad := range []string{"", strings.Repeat("x", MaxPolicyBytes+1), "a\x00b"} {
		_, err := PreviewPolicy(ctx, c, "acme/payments", bad)
		assert.ErrorIs(t, err, ErrBadInput)
	}
	assert.Equal(t, n, f.count("Validate"), "a bad draft is not sent")
}

func TestProposePolicy(t *testing.T) {
	f, c := startFake(t)
	change := make([]byte, 16)
	change[15] = 0x01
	changeset := make([]byte, 32)
	changeset[0] = 0x55
	f.propose = func(req *ascv1.ProposePolicyRequest) (*ascv1.ProposePolicyResponse, error) {
		return &ascv1.ProposePolicyResponse{Change: change, Changeset: changeset}, nil
	}
	base := make([]byte, 32)
	base[0] = 0xab
	op := NewOpID()

	p, err := ProposePolicy(userCtx(t, "tok-maintainer"), c, "acme/payments", base, boardPolicyText, "policy: require signed", op)
	require.NoError(t, err)
	assert.Equal(t, strings.Repeat("z", 31)+"y", p.Change)
	assert.Equal(t, "zzzzzzzzzzzz", p.Short)
	assert.Equal(t, changeset, p.Changeset)

	call := f.last(t, "Propose")
	assert.Equal(t, "Bearer tok-maintainer", call.Auth, "a proposal is made as the person: an agent proposes as an agent")
	assert.Equal(t, op, call.OpID, "a double submit has the same op id")
	req := call.Req.(*ascv1.ProposePolicyRequest)
	assert.Equal(t, base, req.GetBase())
	assert.Equal(t, boardPolicyText, string(req.GetText()))
	assert.Equal(t, "policy: require signed", string(req.GetMessage()))
	assert.Equal(t, "acme/payments", req.GetRepo().GetName())
}

func TestProposePolicyRefusesBadInputBeforeTheCall(t *testing.T) {
	f, c := startFake(t)
	f.propose = func(*ascv1.ProposePolicyRequest) (*ascv1.ProposePolicyResponse, error) {
		return &ascv1.ProposePolicyResponse{Change: make([]byte, 16), Changeset: make([]byte, 32)}, nil
	}
	ctx := userCtx(t, "t")
	good := make([]byte, 32)
	op := NewOpID()
	cases := []struct {
		name         string
		base         []byte
		text, msg, o string
	}{
		{"empty text", good, "", "", op},
		{"too large", good, strings.Repeat("x", MaxPolicyBytes+1), "", op},
		{"nul byte", good, "a\x00", "", op},
		{"short base", []byte{1}, boardPolicyText, "", op},
		{"op id", good, boardPolicyText, "", "not-a-uuid"},
		{"long message", good, boardPolicyText, strings.Repeat("m", 1025), op},
		{"control in message", good, boardPolicyText, "\x00\x01", op},
	}
	for _, tc := range cases {
		_, err := ProposePolicy(ctx, c, "acme/payments", tc.base, tc.text, tc.msg, tc.o)
		assert.ErrorIs(t, err, ErrBadInput, tc.name)
	}
	assert.Zero(t, f.count("Propose"), "nothing reached the server")
}

func TestProposePolicyPassesRefusalsOnAsTheirKind(t *testing.T) {
	f, c := startFake(t)
	for _, tc := range []struct {
		code   codes.Code
		detail string
		kind   Kind
		msg    string
	}{
		{codes.FailedPrecondition, "VERSION_CONFLICT", KindConflict, "trunk moved"},
		{codes.FailedPrecondition, "POLICY_LOCKOUT", KindConflict, "nobody who could change it"},
		{codes.InvalidArgument, "POLICY_INVALID", KindInvalid, "not valid"},
		{codes.PermissionDenied, "POLICY_DENIED", KindDenied, "does not allow"},
	} {
		st, err := status.New(tc.code, "internal: row 17 of op_results").WithDetails(&ascv1.ErrorDetail{Code: tc.detail})
		require.NoError(t, err)
		f.propose = func(*ascv1.ProposePolicyRequest) (*ascv1.ProposePolicyResponse, error) { return nil, st.Err() }
		_, err = ProposePolicy(userCtx(t, "t"), c, "acme/payments", nil, boardPolicyText, "", NewOpID())
		require.Error(t, err)
		e := Classify(err)
		assert.Equal(t, tc.kind, e.Kind, tc.detail)
		assert.Contains(t, e.Message(), tc.msg, tc.detail)
		assert.NotContains(t, e.Message(), "op_results")
	}
}

func TestProposePolicyChecksTheAnswer(t *testing.T) {
	f, c := startFake(t)
	f.propose = func(*ascv1.ProposePolicyRequest) (*ascv1.ProposePolicyResponse, error) {
		return &ascv1.ProposePolicyResponse{Change: []byte{1, 2}, Changeset: make([]byte, 32)}, nil
	}
	_, err := ProposePolicy(userCtx(t, "t"), c, "acme/payments", nil, boardPolicyText, "", NewOpID())
	assert.ErrorIs(t, err, ErrBadAnswer)
}

func TestChangeText(t *testing.T) {
	assert.Equal(t, strings.Repeat("z", 32), ChangeText(make([]byte, 16)))
	ff := make([]byte, 16)
	for i := range ff {
		ff[i] = 0xff
	}
	assert.Equal(t, strings.Repeat("k", 32), ChangeText(ff), "0xf is the letter k")
	assert.Empty(t, ChangeText(make([]byte, 15)))
	assert.Empty(t, ChangeText(nil))
}

func TestRequiredChecksAndRequireSigned(t *testing.T) {
	f, c := startFake(t)
	f.getSource = func(*ascv1.GetPolicySourceRequest) (*ascv1.GetPolicySourceResponse, error) {
		return policySource(), nil
	}
	got, err := RequiredChecks(System(t.Context()), c, "acme/payments")
	require.NoError(t, err)
	assert.Equal(t, []string{"unit", "lint"}, got)
	assert.Equal(t, "Bearer "+testInternalToken, f.last(t, "GetSource").Auth)
	assert.True(t, RequireSigned(System(t.Context()), c, "acme/payments"))

	f.getSource = func(*ascv1.GetPolicySourceRequest) (*ascv1.GetPolicySourceResponse, error) {
		return &ascv1.GetPolicySourceResponse{Present: false}, nil
	}
	got, err = RequiredChecks(System(t.Context()), c, "acme/payments")
	require.NoError(t, err)
	assert.Empty(t, got)
	assert.False(t, RequireSigned(System(t.Context()), c, "acme/payments"))

	f.getSource = func(*ascv1.GetPolicySourceRequest) (*ascv1.GetPolicySourceResponse, error) {
		return nil, status.Error(codes.Unavailable, "down")
	}
	assert.False(t, RequireSigned(System(t.Context()), c, "acme/payments"), "a policy that cannot be read does not claim signing is required")
}

func TestHasFeatureAsksTheServerAndRemembers(t *testing.T) {
	f, _ := startFake(t)
	f.version = func() *ascv1.VersionResponse { return &ascv1.VersionResponse{Features: []string{FeatureAudit}} }
	assert.True(t, HasFeature(t.Context(), FeatureAudit))
	assert.False(t, HasFeature(t.Context(), FeatureSigning))
	assert.False(t, HasFeature(t.Context(), "unknown"))
	assert.Equal(t, 1, f.count("Version"), "the answer is kept for a minute")
	assert.Equal(t, "Bearer "+testInternalToken, f.last(t, "Version").Auth)
}

func TestHasFeatureIsFalseWhenTheServerCannotBeAsked(t *testing.T) {
	defer UseConn(stubConn{err: status.Error(codes.Unavailable, "down")})()
	assert.False(t, HasFeature(t.Context(), FeatureAudit))
}

func TestRepoNameNeedsAscAndAnAscRepository(t *testing.T) {
	defer test.MockVariableValue(&setting.Asc.Enabled, true)()
	defer test.MockVariableValue(&setting.RepoRootPath, t.TempDir())()
	repo := &repo_model.Repository{OwnerName: "Acme", Name: "Payments", StorageBackend: "asc"}
	name, err := RepoName(repo)
	require.NoError(t, err)
	assert.Equal(t, "acme/payments", name)

	git := &repo_model.Repository{OwnerName: "Acme", Name: "Payments", StorageBackend: "git"}
	_, err = RepoName(git)
	assert.ErrorIs(t, err, ErrDisabled)
	_, err = RepoName(&repo_model.Repository{OwnerName: "Acme", Name: "Payments"})
	assert.ErrorIs(t, err, ErrDisabled, "an unset backend is git")
	_, err = RepoName(nil)
	assert.ErrorIs(t, err, ErrDisabled)

	setting.Asc.Enabled = false
	_, err = RepoName(repo)
	assert.ErrorIs(t, err, ErrDisabled, "[asc] off")
}

func TestQuarantineAllowedFailsClosed(t *testing.T) {
	f, c := startFake(t)
	serve := func(text string, present bool, parseErr string) {
		f.getSource = func(*ascv1.GetPolicySourceRequest) (*ascv1.GetPolicySourceResponse, error) {
			return &ascv1.GetPolicySourceResponse{Present: present, Text: []byte(text), Parsed: &ascv1.GetPolicyResponse{Error: parseErr}}, nil
		}
	}
	serve("version = 1\n[ci]\nallow_quarantine = true\n", true, "")
	assert.True(t, QuarantineAllowed(userCtx(t, "t"), c, "acme/payments"))
	serve("version = 1\n[ci]\nallow_quarantine = false\n", true, "")
	assert.False(t, QuarantineAllowed(userCtx(t, "t"), c, "acme/payments"))
	serve("version = 1\n", true, "")
	assert.False(t, QuarantineAllowed(userCtx(t, "t"), c, "acme/payments"), "not set means no")
	serve("version = 1\n[ci]\nallow_quarantine = true\n", false, "")
	assert.False(t, QuarantineAllowed(userCtx(t, "t"), c, "acme/payments"), "no policy file")
	serve("version = 1\n[ci]\nallow_quarantine = true\n", true, "bad file")
	assert.False(t, QuarantineAllowed(userCtx(t, "t"), c, "acme/payments"), "a policy that cannot be used")
	f.getSource = func(*ascv1.GetPolicySourceRequest) (*ascv1.GetPolicySourceResponse, error) {
		return nil, status.Error(codes.Unavailable, "down")
	}
	assert.False(t, QuarantineAllowed(userCtx(t, "t"), c, "acme/payments"), "an error is no")
}
