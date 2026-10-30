// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package pull

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"testing"
	"time"

	issues_model "forgejo.org/models/issues"
	repo_model "forgejo.org/models/repo"
	user_model "forgejo.org/models/user"
	ascv1 "forgejo.org/modules/ascproto/ascv1"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
)

type fakeAmender struct {
	*fakeChanges
	reqs []*ascv1.AmendChangeRequest
	resp *ascv1.AmendChangeResponse
	err  error
	// after runs when the call is made, before it answers.
	after func()
}

func (f *fakeAmender) AmendChange(_ context.Context, in *ascv1.AmendChangeRequest, _ ...grpc.CallOption) (*ascv1.AmendChangeResponse, error) {
	f.reqs = append(f.reqs, in)
	if f.after != nil {
		f.after()
	}
	return f.resp, f.err
}

type fakeSuggestReview struct {
	ascv1.ReviewClient
	prep     *ascv1.PrepareSuggestionResponse
	prepErr  error
	prepared []*ascv1.PrepareSuggestionRequest
	marked   []*ascv1.MarkSuggestionAppliedRequest
	markErr  error
	threads  []*ascv1.ReviewThread
}

func (f *fakeSuggestReview) PrepareSuggestion(_ context.Context, in *ascv1.PrepareSuggestionRequest, _ ...grpc.CallOption) (*ascv1.PrepareSuggestionResponse, error) {
	f.prepared = append(f.prepared, in)
	return f.prep, f.prepErr
}

func (f *fakeSuggestReview) MarkSuggestionApplied(_ context.Context, in *ascv1.MarkSuggestionAppliedRequest, _ ...grpc.CallOption) (*ascv1.MarkSuggestionAppliedResponse, error) {
	f.marked = append(f.marked, in)
	return &ascv1.MarkSuggestionAppliedResponse{}, f.markErr
}

func (f *fakeSuggestReview) ListThreads(_ context.Context, _ *ascv1.ListThreadsRequest, _ ...grpc.CallOption) (*ascv1.ListThreadsResponse, error) {
	return &ascv1.ListThreadsResponse{Threads: f.threads}, nil
}

type suggestionWorld struct {
	pr      *issues_model.PullRequest
	changes *fakeAmender
	review  *fakeSuggestReview
	heads   *fakeHeads
	calls   *[]followUpCall
}

// setupAscSuggestion is setupAscRestore (versions a, b, c, the head at c) with a Review and an AmendChange that answer
// "applied as version 4, commit d".
func setupAscSuggestion(t *testing.T) *suggestionWorld {
	t.Helper()
	pr, fc, heads, calls := setupAscRestore(t)
	w := &suggestionWorld{
		pr: pr, heads: heads, calls: calls,
		changes: &fakeAmender{fakeChanges: fc, resp: &ascv1.AmendChangeResponse{
			Version: 4, LatestVersion: 4, Changeset: bytes.Repeat([]byte{4}, 32), Git: mustHex(t, gitHexN('d')),
		}},
		review: &fakeSuggestReview{prep: &ascv1.PrepareSuggestionResponse{Path: []byte("src/a.rs"), Version: 3, LatestVersion: 3, NewContent: []byte("new\n")}},
	}
	heads.commits[gitHexN('d')] = true
	prevC, prevR, prevU := ascChangesFor, ascReviewFor, ascUserCall
	ascChangesFor = func() (ascv1.ChangesClient, error) { return w.changes, nil }
	ascReviewFor = func() (ascv1.ReviewClient, error) { return w.review, nil }
	ascUserCall = func(ctx context.Context, _ *user_model.User, _ *repo_model.Repository) (context.Context, bool, error) {
		return ctx, true, nil
	}
	prevWait, prevPoll := ascSuggestionWait, ascSuggestionPoll
	ascSuggestionWait, ascSuggestionPoll = 30*time.Millisecond, 5*time.Millisecond
	t.Cleanup(func() {
		ascChangesFor, ascReviewFor, ascUserCall = prevC, prevR, prevU
		ascSuggestionWait, ascSuggestionPoll = prevWait, prevPoll
	})
	return w
}

func mustHex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(s)
	require.NoError(t, err)
	return b
}

func TestAscApplySuggestionMakesTheNextVersionAndMovesTheHead(t *testing.T) {
	w := setupAscSuggestion(t)
	admin := ascTestUser(t, 1)

	res, err := AscApplySuggestion(t.Context(), w.pr, admin, 11, 22)
	require.NoError(t, err)
	assert.EqualValues(t, 4, res.Version)
	assert.Equal(t, gitHexN('d'), res.Commit)
	assert.False(t, res.Pending)
	assert.False(t, res.MarkFailed)

	require.Len(t, w.review.prepared, 1)
	assert.EqualValues(t, 11, w.review.prepared[0].GetThreadId())
	assert.EqualValues(t, 22, w.review.prepared[0].GetCommentId())
	require.Len(t, w.changes.reqs, 1)
	req := w.changes.reqs[0]
	assert.EqualValues(t, 3, req.GetBaseVersion(), "the version the suggestion was prepared on")
	assert.Equal(t, fmt.Sprintf("refs/pull/%d/head", w.pr.Index), req.GetUpdateRef(), "only the pull head moves")
	require.Len(t, req.GetFiles(), 1)
	assert.Equal(t, "src/a.rs", string(req.GetFiles()[0].GetPath()))
	assert.Equal(t, "new\n", string(req.GetFiles()[0].GetContent()))
	assert.Equal(t, admin.Name, req.GetCommitterName(), "the committer is the person")
	assert.NotEmpty(t, req.GetCommitterEmail())

	assert.Equal(t, []string{gitHexN('d')}, w.heads.moves)
	assert.Equal(t, []followUpCall{{doerID: 1, oldID: gitHexN('c'), newID: gitHexN('d')}}, *w.calls)
	require.Len(t, w.review.marked, 1)
	assert.EqualValues(t, 4, w.review.marked[0].GetVersion())
}

func TestAscApplySuggestionLeavesAHeadAscMovedAlone(t *testing.T) {
	w := setupAscSuggestion(t)
	// asc's own post-receive moves the pull head to the new commit while the call is made.
	w.changes.after = func() { w.heads.head = gitHexN('d') }
	res, err := AscApplySuggestion(t.Context(), w.pr, ascTestUser(t, 1), 11, 22)
	require.NoError(t, err)
	assert.EqualValues(t, 4, res.Version)
	assert.Empty(t, w.heads.moves, "the head is at the commit already")
	assert.Empty(t, *w.calls, "and the follow-up of a push ran when it moved")
	assert.Len(t, w.review.marked, 1)
}

func TestAscApplySuggestionStaleWritesNothing(t *testing.T) {
	admin := ascTestUser(t, 1)

	t.Run("the suggestion no longer fits", func(t *testing.T) {
		w := setupAscSuggestion(t)
		w.review.prep = &ascv1.PrepareSuggestionResponse{Stale: true, Reason: "the lines are no longer in the file"}
		_, err := AscApplySuggestion(t.Context(), w.pr, admin, 11, 22)
		var stale *AscSuggestionStale
		require.ErrorAs(t, err, &stale)
		assert.Contains(t, stale.Reason, "no longer in the file")
		assert.Empty(t, w.changes.reqs)
		assert.Empty(t, w.review.marked)
		assert.Empty(t, w.heads.moves)
	})
	t.Run("the thread is on an older version", func(t *testing.T) {
		w := setupAscSuggestion(t)
		w.review.prep.Version = 2
		_, err := AscApplySuggestion(t.Context(), w.pr, admin, 11, 22)
		var stale *AscSuggestionStale
		require.ErrorAs(t, err, &stale)
		assert.Empty(t, w.changes.reqs)
	})
	t.Run("a push came first", func(t *testing.T) {
		w := setupAscSuggestion(t)
		w.changes.resp = &ascv1.AmendChangeResponse{Stale: true, Reason: "the pull request moved since it was looked at", Version: 3, LatestVersion: 3}
		_, err := AscApplySuggestion(t.Context(), w.pr, admin, 11, 22)
		var stale *AscSuggestionStale
		require.ErrorAs(t, err, &stale)
		assert.Contains(t, stale.Reason, "moved")
		assert.Empty(t, w.review.marked, "a suggestion that did not apply is not marked")
		assert.Empty(t, w.heads.moves)
		assert.Empty(t, *w.calls)
	})
	t.Run("the pull request was restored to an older version", func(t *testing.T) {
		w := setupAscSuggestion(t)
		w.heads.head = gitHexN('b')
		_, err := AscApplySuggestion(t.Context(), w.pr, admin, 11, 22)
		var stale *AscSuggestionStale
		require.ErrorAs(t, err, &stale)
		assert.Empty(t, w.review.prepared, "asc is not asked")
		assert.Empty(t, w.changes.reqs)
	})
}

func TestAscApplySuggestionRefusals(t *testing.T) {
	t.Run("a person with no right to update the pull request", func(t *testing.T) {
		w := setupAscSuggestion(t)
		_, err := AscApplySuggestion(t.Context(), w.pr, ascTestUser(t, 4), 11, 22)
		require.ErrorIs(t, err, ErrAscRestoreForbidden)
		assert.Empty(t, w.review.prepared)
		assert.Empty(t, w.changes.reqs)
	})
	t.Run("ids that are not ids", func(t *testing.T) {
		w := setupAscSuggestion(t)
		_, err := AscApplySuggestion(t.Context(), w.pr, ascTestUser(t, 1), 0, 22)
		require.ErrorIs(t, err, ErrAscBadVersion)
		_, err = AscApplySuggestion(t.Context(), w.pr, ascTestUser(t, 1), 11, 0)
		require.ErrorIs(t, err, ErrAscBadVersion)
	})
	t.Run("the hub does not allow the person's call", func(t *testing.T) {
		w := setupAscSuggestion(t)
		ascUserCall = func(ctx context.Context, _ *user_model.User, _ *repo_model.Repository) (context.Context, bool, error) {
			return ctx, false, nil
		}
		_, err := AscApplySuggestion(t.Context(), w.pr, ascTestUser(t, 1), 11, 22)
		require.ErrorIs(t, err, ErrAscRestoreForbidden)
		assert.Empty(t, w.changes.reqs)
	})
	t.Run("asc cannot be reached", func(t *testing.T) {
		w := setupAscSuggestion(t)
		w.changes.err = errors.New("down")
		_, err := AscApplySuggestion(t.Context(), w.pr, ascTestUser(t, 1), 11, 22)
		require.ErrorIs(t, err, ErrAscUnavailable)
		assert.Empty(t, w.review.marked)
	})
}

func TestAscApplySuggestionWaitsForTheCommitOnlyBriefly(t *testing.T) {
	w := setupAscSuggestion(t)
	delete(w.heads.commits, gitHexN('d'))
	res, err := AscApplySuggestion(t.Context(), w.pr, ascTestUser(t, 1), 11, 22)
	require.NoError(t, err)
	assert.True(t, res.Pending, "the commit is not in the repository yet")
	assert.Empty(t, w.heads.moves)
	assert.Len(t, w.review.marked, 1, "the version exists, so the suggestion is marked")
}

func TestAscApplySuggestionReportsAMarkThatFailed(t *testing.T) {
	w := setupAscSuggestion(t)
	w.review.markErr = errors.New("busy")
	res, err := AscApplySuggestion(t.Context(), w.pr, ascTestUser(t, 1), 11, 22)
	require.NoError(t, err)
	assert.True(t, res.MarkFailed)
	assert.EqualValues(t, 4, res.Version)
}

func TestAscSuggestionsListsOpenSuggestionsAndMarksOldOnes(t *testing.T) {
	w := setupAscSuggestion(t)
	anchor := func(v uint32) *ascv1.ThreadAnchor {
		return &ascv1.ThreadAnchor{Path: []byte("src/a.rs"), Version: v, StartLine: 4, EndLine: 6}
	}
	w.review.threads = []*ascv1.ReviewThread{
		{Id: 1, Anchor: anchor(3), Comments: []*ascv1.ReviewComment{{Id: 10, Author: "priya", Body: "tidy", Suggestion: "fn a() {}\n"}}},
		{Id: 2, Anchor: anchor(2), Comments: []*ascv1.ReviewComment{{Id: 20, Author: "bob", Suggestion: "x\n"}}},
		{Id: 3, Anchor: anchor(3), Comments: []*ascv1.ReviewComment{{Id: 30, Author: "carol", Suggestion: "y\n", AppliedVersion: 3}}},
		{Id: 4, Anchor: anchor(3), Comments: []*ascv1.ReviewComment{{Id: 40, Author: "dave", Body: "no suggestion"}}},
		{Id: 5, Anchor: &ascv1.ThreadAnchor{}, Comments: []*ascv1.ReviewComment{{Id: 50, Suggestion: "z\n"}}},
	}
	got, err := AscSuggestions(t.Context(), w.pr)
	require.NoError(t, err)
	require.Len(t, got, 2, "the applied one, the plain comment and the thread with no lines are left out")
	assert.Equal(t, AscSuggestion{ThreadID: 1, CommentID: 10, Path: "src/a.rs", StartLine: 4, EndLine: 6, Author: "priya", Body: "tidy", Suggestion: "fn a() {}\n", Version: 3}, got[0])
	assert.True(t, got[1].Stale, "a thread on an older version cannot be applied")
	assert.Equal(t, "4-6", got[0].Lines())
	assert.Equal(t, "fn a() {}", got[0].Preview())
}
