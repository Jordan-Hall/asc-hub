// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package pull

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"io"
	"strings"
	"testing"

	"forgejo.org/models/db"
	issues_model "forgejo.org/models/issues"
	"forgejo.org/models/unittest"
	ascv1 "forgejo.org/modules/ascproto/ascv1"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
)

type fakeDiffStream struct {
	grpc.ClientStream
	msgs []*ascv1.DiffResponse
	err  error
}

func (s *fakeDiffStream) Recv() (*ascv1.DiffResponse, error) {
	if len(s.msgs) == 0 {
		if s.err != nil {
			return nil, s.err
		}
		return nil, io.EOF
	}
	m := s.msgs[0]
	s.msgs = s.msgs[1:]
	return m, nil
}

type fakeChanges struct {
	ascv1.ChangesClient
	versions   []*ascv1.Version
	pageSize   int
	diffMsgs   []*ascv1.DiffResponse
	diffCalls  int
	lastChange []byte
	lastDiff   *ascv1.InterDiffRequest
}

func (f *fakeChanges) Versions(_ context.Context, in *ascv1.VersionsRequest, _ ...grpc.CallOption) (*ascv1.VersionsResponse, error) {
	f.lastChange = in.GetChange()
	start := 0
	if in.GetPageToken() != "" {
		for i, v := range f.versions {
			if in.GetPageToken() == string(rune('a'+v.GetV())) {
				start = i + 1
			}
		}
	}
	end := len(f.versions)
	next := ""
	if f.pageSize > 0 && start+f.pageSize < end {
		end = start + f.pageSize
		next = string(rune('a' + f.versions[end-1].GetV()))
	}
	return &ascv1.VersionsResponse{Versions: f.versions[start:end], NextPageToken: next}, nil
}

func (f *fakeChanges) InterDiff(_ context.Context, in *ascv1.InterDiffRequest, _ ...grpc.CallOption) (grpc.ServerStreamingClient[ascv1.DiffResponse], error) {
	f.diffCalls++
	f.lastDiff = in
	return &fakeDiffStream{msgs: f.diffMsgs}, nil
}

func ascTestVersion(n uint32, gitHex string) *ascv1.Version {
	g, _ := hex.DecodeString(gitHex)
	return &ascv1.Version{V: n, Cs: bytes.Repeat([]byte{byte(n)}, 32), Git: g, At: int64(n), Actor: "a"}
}

func gitHexN(c byte) string { return strings.Repeat(string(c), 40) }

func setupAscVersions(t *testing.T, f *fakeChanges) *issues_model.PullRequest {
	t.Helper()
	require.NoError(t, unittest.PrepareTestDatabase())
	pr := unittest.AssertExistsAndLoadBean(t, &issues_model.PullRequest{ID: 5})
	require.NoError(t, issues_model.SetPullRequestAscStack(t.Context(), pr, ascTestChange, 0))
	prevC, prevR := ascChangesFor, ascRepoNameOf
	ascChangesFor = func() (ascv1.ChangesClient, error) { return f, nil }
	ascRepoNameOf = func(context.Context, *issues_model.PullRequest) (string, error) { return "owner/repo", nil }
	t.Cleanup(func() { ascChangesFor, ascRepoNameOf = prevC, prevR })
	return pr
}

func threeVersions() []*ascv1.Version {
	return []*ascv1.Version{ascTestVersion(1, gitHexN('a')), ascTestVersion(2, gitHexN('b')), ascTestVersion(3, gitHexN('c'))}
}

func TestAscVersionsList(t *testing.T) {
	f := &fakeChanges{versions: threeVersions(), pageSize: 2}
	pr := setupAscVersions(t, f)

	l, err := AscVersions(t.Context(), pr)
	require.NoError(t, err)
	assert.Len(t, l.Versions, 3)
	assert.EqualValues(t, 3, l.Latest())
	assert.Equal(t, 16, len(f.lastChange))
	assert.EqualValues(t, 2, l.ByGitCommit(gitHexN('B')))
	assert.EqualValues(t, 0, l.ByGitCommit(gitHexN('d')))
	assert.EqualValues(t, 0, l.ByGitCommit("zz"))
}

func TestAscVersionSteps(t *testing.T) {
	var nilList *AscVersionList
	assert.Empty(t, nilList.Steps())
	assert.Empty(t, (&AscVersionList{}).Steps())
	assert.Empty(t, (&AscVersionList{Versions: []AscVersion{{N: 1}}}).Steps())
	l := &AscVersionList{Versions: []AscVersion{{N: 1}, {N: 2}, {N: 3}}}
	assert.Equal(t, []AscVersionStep{{From: 1, To: 2}, {From: 2, To: 3}}, l.Steps())
}

func TestAscVersionsNotStacked(t *testing.T) {
	f := &fakeChanges{versions: threeVersions()}
	pr := setupAscVersions(t, f)
	other := unittest.AssertExistsAndLoadBean(t, &issues_model.PullRequest{ID: 2})
	_, err := AscVersions(t.Context(), other)
	require.ErrorIs(t, err, ErrAscNotStacked)
	_, err = AscInterDiffOf(t.Context(), other, 1, 2, nil)
	require.ErrorIs(t, err, ErrAscNotStacked)
	assert.Zero(t, f.diffCalls)

	ascRepoNameOf = func(context.Context, *issues_model.PullRequest) (string, error) { return "", nil }
	_, err = AscVersions(t.Context(), pr)
	require.ErrorIs(t, err, ErrAscNotStacked)
}

func TestAscVersionsRefuseBadAnswers(t *testing.T) {
	for name, vs := range map[string][]*ascv1.Version{
		"gap":       {ascTestVersion(1, gitHexN('a')), ascTestVersion(3, gitHexN('b'))},
		"duplicate": {ascTestVersion(1, gitHexN('a')), ascTestVersion(1, gitHexN('b'))},
		"short git": {ascTestVersion(1, gitHexN('a')[:38])},
		"nil":       {nil},
	} {
		t.Run(name, func(t *testing.T) {
			pr := setupAscVersions(t, &fakeChanges{versions: vs})
			_, err := AscVersions(t.Context(), pr)
			require.ErrorIs(t, err, ErrAscBadAnswer)
		})
	}
}

func TestAscInterDiffChecksVersionsAndPaths(t *testing.T) {
	f := &fakeChanges{versions: threeVersions(), diffMsgs: []*ascv1.DiffResponse{
		{Changes: []*ascv1.PathChange{{Path: []byte("a.txt")}}, Text: []byte("diff one\n")},
		{Changes: []*ascv1.PathChange{{Path: []byte("a.txt")}, {Path: []byte("b.txt")}}, Text: []byte("diff two\n")},
	}}
	pr := setupAscVersions(t, f)
	ctx := t.Context()

	for _, bad := range [][2]uint32{{0, 1}, {1, 4}, {4, 1}, {2, 2}} {
		_, err := AscInterDiffOf(ctx, pr, bad[0], bad[1], nil)
		require.ErrorIs(t, err, ErrAscBadVersion, "%v", bad)
	}
	assert.Zero(t, f.diffCalls, "asc is not asked for versions the change lacks")

	_, err := AscInterDiffOf(ctx, pr, 1, 2, []string{"ok", "bad\x00path"})
	require.ErrorIs(t, err, ErrAscBadPaths)
	_, err = AscInterDiffOf(ctx, pr, 1, 2, make([]string, AscMaxInterDiffPaths+1))
	require.ErrorIs(t, err, ErrAscBadPaths)
	assert.Zero(t, f.diffCalls)

	d, err := AscInterDiffOf(ctx, pr, 1, 3, []string{"a.txt"})
	require.NoError(t, err)
	assert.Equal(t, "diff one\ndiff two\n", string(d.Patch))
	assert.Equal(t, []string{"a.txt", "b.txt"}, d.Paths)
	assert.False(t, d.Truncated)
	assert.EqualValues(t, 1, f.lastDiff.GetFromV())
	assert.EqualValues(t, 3, f.lastDiff.GetToV())
	assert.Equal(t, ascv1.DiffFormat_DIFF_FORMAT_PATCH, f.lastDiff.GetFormat())
	assert.Equal(t, "owner/repo", f.lastDiff.GetRepo().GetName())
	assert.Equal(t, f.lastChange, f.lastDiff.GetChange(), "the change is the pull request's own")
}

func TestAscInterDiffBoundsAndBadPaths(t *testing.T) {
	big := bytes.Repeat([]byte("x"), AscMaxInterDiffBytes+10)
	f := &fakeChanges{versions: threeVersions(), diffMsgs: []*ascv1.DiffResponse{{Text: big}}}
	pr := setupAscVersions(t, f)
	d, err := AscInterDiffOf(t.Context(), pr, 1, 2, nil)
	require.NoError(t, err)
	assert.True(t, d.Truncated)
	assert.Len(t, d.Patch, AscMaxInterDiffBytes)

	f.diffMsgs = []*ascv1.DiffResponse{{Changes: []*ascv1.PathChange{{Path: []byte("a\x00b")}}}}
	_, err = AscInterDiffOf(t.Context(), pr, 1, 2, nil)
	require.ErrorIs(t, err, ErrAscBadAnswer)
}

func addAscReview(t *testing.T, pr *issues_model.PullRequest, reviewer int64, typ issues_model.ReviewType, commit string) {
	t.Helper()
	require.NoError(t, pr.LoadIssue(t.Context()))
	_, err := db.GetEngine(t.Context()).Insert(&issues_model.Review{
		Type: typ, ReviewerID: reviewer, IssueID: pr.Issue.ID, CommitID: commit,
	})
	require.NoError(t, err)
}

func TestAscSinceLastReview(t *testing.T) {
	f := &fakeChanges{versions: threeVersions(), diffMsgs: []*ascv1.DiffResponse{{Text: []byte("since\n")}}}
	pr := setupAscVersions(t, f)
	ctx := t.Context()
	const reviewer = int64(900001)

	// No review: nothing to show, asc is not asked for a diff.
	d, err := AscChangesSinceLastReview(ctx, pr, reviewer, nil)
	require.NoError(t, err)
	assert.Nil(t, d)

	// A review of version 1, then a pending draft and a request that must not count.
	addAscReview(t, pr, reviewer, issues_model.ReviewTypeApprove, gitHexN('a'))
	addAscReview(t, pr, reviewer, issues_model.ReviewTypePending, gitHexN('c'))
	addAscReview(t, pr, reviewer, issues_model.ReviewTypeRequest, gitHexN('c'))
	seen, _, err := AscSinceLastReview(ctx, pr, reviewer)
	require.NoError(t, err)
	assert.EqualValues(t, 1, seen)

	d, err = AscChangesSinceLastReview(ctx, pr, reviewer, nil)
	require.NoError(t, err)
	require.NotNil(t, d)
	assert.EqualValues(t, 1, d.From)
	assert.EqualValues(t, 3, d.To)
	assert.Equal(t, "since\n", string(d.Patch))

	// Another user has not reviewed: no "since" for them.
	d, err = AscChangesSinceLastReview(ctx, pr, 900002, nil)
	require.NoError(t, err)
	assert.Nil(t, d)

	// A review of a commit that is not a version of the change is not "since" anything.
	addAscReview(t, pr, 900003, issues_model.ReviewTypeComment, gitHexN('f'))
	seen, _, err = AscSinceLastReview(ctx, pr, 900003)
	require.NoError(t, err)
	assert.Zero(t, seen)

	// A review of the latest version: nothing new.
	addAscReview(t, pr, 900004, issues_model.ReviewTypeReject, gitHexN('c'))
	d, err = AscChangesSinceLastReview(ctx, pr, 900004, nil)
	require.NoError(t, err)
	assert.Nil(t, d)
}

func TestAscSinceLastReviewAscFailure(t *testing.T) {
	pr := setupAscVersions(t, &fakeChanges{versions: threeVersions()})
	ascChangesFor = func() (ascv1.ChangesClient, error) { return nil, errors.New("no token") }
	_, _, err := AscSinceLastReview(t.Context(), pr, 1)
	require.ErrorIs(t, err, ErrAscUnavailable)
}
