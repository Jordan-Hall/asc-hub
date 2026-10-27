// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package asctrust

import (
	"context"
	"errors"
	"strings"
	"testing"

	issues_model "forgejo.org/models/issues"
	repo_model "forgejo.org/models/repo"
	user_model "forgejo.org/models/user"
	ascv1 "forgejo.org/modules/ascproto/ascv1"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
)

type seamLog struct {
	created []string
	commit  string
	has     bool
	open    *issues_model.PullRequest
	err     error
}

func (l *seamLog) seams() PullSeams {
	return PullSeams{
		GitCommit: func(context.Context, grpc.ClientConnInterface, string, []byte) (string, error) {
			return l.commit, l.err
		},
		HasCommit: func(context.Context, *repo_model.Repository, string) (bool, error) { return l.has, nil },
		FindOpen: func(context.Context, *repo_model.Repository, string) (*issues_model.PullRequest, error) {
			return l.open, nil
		},
		Create: func(_ context.Context, _ *repo_model.Repository, _ *user_model.User, change, commit, title, body string) (*issues_model.PullRequest, error) {
			l.created = append(l.created, change+"|"+commit+"|"+title+"|"+body)
			return &issues_model.PullRequest{ID: 77}, nil
		},
	}
}

func proposalOf() *Proposal {
	return &Proposal{Change: strings.Repeat("k", 32), Short: strings.Repeat("k", 12), Changeset: make([]byte, 32)}
}

func TestOpenProposalPullOpensOneForTheChange(t *testing.T) {
	l := &seamLog{commit: strings.Repeat("ab", 20), has: true}
	pr, err := OpenProposalPull(t.Context(), nil, &repo_model.Repository{}, "acme/payments", &user_model.User{ID: 2}, proposalOf(), "policy: require signed\n\nlonger text", l.seams())
	require.NoError(t, err)
	assert.EqualValues(t, 77, pr.ID)
	require.Len(t, l.created, 1)
	parts := strings.SplitN(l.created[0], "|", 4)
	assert.Equal(t, strings.Repeat("k", 32), parts[0])
	assert.Equal(t, strings.Repeat("ab", 20), parts[1])
	assert.Equal(t, "policy: require signed", parts[2], "the title is the first line")
	assert.Contains(t, parts[3], ".asc/policy.toml")
	assert.Contains(t, parts[3], "Nothing changes on trunk until then")
}

func TestOpenProposalPullDefaultTitle(t *testing.T) {
	l := &seamLog{commit: strings.Repeat("ab", 20), has: true}
	_, err := OpenProposalPull(t.Context(), nil, &repo_model.Repository{}, "acme/payments", &user_model.User{ID: 2}, proposalOf(), "", l.seams())
	require.NoError(t, err)
	assert.Contains(t, l.created[0], "|policy: update .asc/policy.toml|")
}

func TestOpenProposalPullFindsTheExistingOne(t *testing.T) {
	l := &seamLog{open: &issues_model.PullRequest{ID: 5}, commit: "unused"}
	pr, err := OpenProposalPull(t.Context(), nil, &repo_model.Repository{}, "acme/payments", &user_model.User{ID: 2}, proposalOf(), "", l.seams())
	require.NoError(t, err)
	assert.EqualValues(t, 5, pr.ID)
	assert.Empty(t, l.created, "a second submit does not open a second pull request")
}

func TestOpenProposalPullWaitsForTheGitView(t *testing.T) {
	// asc has no git commit for the changeset yet.
	l := &seamLog{commit: ""}
	_, err := OpenProposalPull(t.Context(), nil, &repo_model.Repository{}, "acme/payments", &user_model.User{ID: 2}, proposalOf(), "", l.seams())
	assert.ErrorIs(t, err, ErrNotMirrored)
	// The commit is known to asc but the repository does not hold it.
	l = &seamLog{commit: strings.Repeat("ab", 20), has: false}
	_, err = OpenProposalPull(t.Context(), nil, &repo_model.Repository{}, "acme/payments", &user_model.User{ID: 2}, proposalOf(), "", l.seams())
	assert.ErrorIs(t, err, ErrNotMirrored)
	assert.Empty(t, l.created)
}

func TestOpenProposalPullRefusesWhatItCannotTrust(t *testing.T) {
	l := &seamLog{commit: strings.Repeat("ab", 20), has: true}
	_, err := OpenProposalPull(t.Context(), nil, &repo_model.Repository{}, "n", nil, proposalOf(), "", l.seams())
	assert.ErrorIs(t, err, ErrBadInput)
	_, err = OpenProposalPull(t.Context(), nil, &repo_model.Repository{}, "n", &user_model.User{ID: 2}, nil, "", l.seams())
	assert.ErrorIs(t, err, ErrBadInput)
	_, err = OpenProposalPull(t.Context(), nil, &repo_model.Repository{}, "n", &user_model.User{ID: 2}, &Proposal{Change: "not-a-change-id"}, "", l.seams())
	assert.ErrorIs(t, err, ErrBadAnswer)
	assert.Empty(t, l.created)

	l.err = errors.New("asc down")
	_, err = OpenProposalPull(t.Context(), nil, &repo_model.Repository{}, "n", &user_model.User{ID: 2}, proposalOf(), "", l.seams())
	assert.Error(t, err)
	assert.Empty(t, l.created)
}

func TestGitCommitOfAsksGitMap(t *testing.T) {
	f, c := startFake(t)
	cs := make([]byte, 32)
	cs[0] = 1
	gitID := make([]byte, 20)
	for i := range gitID {
		gitID[i] = 0xcd
	}
	f.gitMap = func(req *ascv1.GitMapRequest) (*ascv1.GitMapResponse, error) {
		require.Equal(t, [][]byte{cs}, req.GetAsc())
		return &ascv1.GitMapResponse{AscToGit: [][]byte{gitID}}, nil
	}
	got, err := gitCommitOf(t.Context(), c, "acme/payments", cs)
	require.NoError(t, err, "the call makes itself a system call")
	assert.Equal(t, strings.Repeat("cd", 20), got)

	f.gitMap = func(*ascv1.GitMapRequest) (*ascv1.GitMapResponse, error) {
		return &ascv1.GitMapResponse{AscToGit: [][]byte{nil}}, nil
	}
	got, err = gitCommitOf(System(t.Context()), c, "acme/payments", cs)
	require.NoError(t, err)
	assert.Empty(t, got, "asc has no git commit for it yet")

	f.gitMap = func(*ascv1.GitMapRequest) (*ascv1.GitMapResponse, error) {
		return &ascv1.GitMapResponse{AscToGit: [][]byte{{1, 2, 3}}}, nil
	}
	_, err = gitCommitOf(System(t.Context()), c, "acme/payments", cs)
	assert.ErrorIs(t, err, ErrBadAnswer)
	_, err = gitCommitOf(System(t.Context()), c, "acme/payments", []byte{1})
	assert.ErrorIs(t, err, ErrBadInput)
}

func TestFirstLineIsCleanedAndCut(t *testing.T) {
	assert.Equal(t, "one", firstLine("  one\ntwo"))
	assert.Equal(t, "ab", firstLine("a\u202eb"))
	assert.Len(t, firstLine(strings.Repeat("x", 500)), 200)
	assert.Empty(t, firstLine(""))
}

func TestCleanText(t *testing.T) {
	cases := map[string]string{
		"plain":                       "plain",
		"  spaced  ":                  "spaced",
		"tab\tand\nnewline":           "tab and newline",
		"nul\x00byte":                 "nulbyte",
		"esc\x1b[31mred":              "esc[31mred",
		"rtl\u202eoverride":           "rtloverride",
		"iso\u2066late\u2069d":        "isolated",
		"zero\u200bwidth":             "zerowidth",
		"bom\ufeffhere":               "bomhere",
		"ls\u2028ps\u2029":            "lsps",
		"bad\xffutf8":                 "badutf8",
		"\u00e9\u4e2d\U0001F600 kept": "\u00e9\u4e2d\U0001F600 kept",
		"":                            "",
	}
	for in, want := range cases {
		assert.Equal(t, want, CleanText(in, 100), "%q", in)
	}
	assert.Equal(t, "abc", CleanText("abcdef", 3))
	assert.Equal(t, "\u00e9", CleanText("\u00e9\u00e9", 3), "cut on a character boundary, not in the middle of one")
}

func TestCleanBlockKeepsLines(t *testing.T) {
	assert.Equal(t, "a\nb", CleanBlock("a\r\nb\r\n", 100))
	assert.Equal(t, "a b", CleanBlock("a\tb", 100))
	assert.Equal(t, "ab", CleanBlock("a\x00\x1bb", 100))
	assert.Equal(t, "ab", CleanBlock("a\u202eb", 100))
	assert.Equal(t, "abc", CleanBlock("abcdef", 3))
}
