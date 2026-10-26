// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package pull

import (
	"context"
	"errors"
	"testing"
	"time"

	"forgejo.org/models/db"
	issues_model "forgejo.org/models/issues"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"
	"forgejo.org/services/ascbridge"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	rsOldBottom = "1010101010101010101010101010101010101010"
	rsOldTop    = "2020202020202020202020202020202020202020"
	rsNewBottom = "3030303030303030303030303030303030303030"
	rsNewTop    = "4040404040404040404040404040404040404040"
	rsOnto      = "5050505050505050505050505050505050505050"
	rsOtherChg  = "oooooooooooooooooooooooooooooooo"

	// the asc ids (hex) of the old heads, as the bridge keeps them for the pin
	rsIDBottom = "1111111111111111111111111111111111111111111111111111111111111111"
	rsIDTop    = "2222222222222222222222222222222222222222222222222222222222222222"
)

type restackMove struct {
	pullID   int64
	from, to string
}

type restackHeads struct {
	heads   map[int64]string
	moves   []restackMove
	moveErr map[int64]error
}

func (f *restackHeads) Head(_ context.Context, pr *issues_model.PullRequest) (string, error) {
	return f.heads[pr.ID], nil
}

func (f *restackHeads) HasCommit(context.Context, *issues_model.PullRequest, string) (bool, error) {
	return true, nil
}

func (f *restackHeads) Move(_ context.Context, pr *issues_model.PullRequest, id string) error {
	if err := f.moveErr[pr.ID]; err != nil {
		return err
	}
	f.moves = append(f.moves, restackMove{pr.ID, f.heads[pr.ID], id})
	f.heads[pr.ID] = id
	return nil
}

type restackFixture struct {
	bottom, top *issues_model.PullRequest
	ops         *fakeStackOps
	heads       *restackHeads
	followUps   []restackMove
}

func newRestackFixture(t *testing.T) *restackFixture {
	t.Helper()
	bottom, top, _ := ascMergeFixture(t)
	f := &restackFixture{bottom: bottom, top: top, ops: &fakeStackOps{}}
	f.heads = &restackHeads{heads: map[int64]string{bottom.ID: rsOldBottom, top.ID: rsOldTop}, moveErr: map[int64]error{}}
	rewritten := func(head bool) *ascbridge.RestackStackResult {
		r := &ascbridge.RestackStackResult{Onto: rsOnto, Items: []ascbridge.RestackItem{
			{Change: ascMergeBottom, State: ascbridge.RestackRewritten, OldHead: rsOldBottom, OldHeadID: rsIDBottom, NewVersion: 2},
			{Change: ascMergeTop, State: ascbridge.RestackRewritten, OldHead: rsOldTop, OldHeadID: rsIDTop, NewVersion: 2},
		}}
		if head {
			r.Applied = true
			r.Items[0].NewHead, r.Items[1].NewHead = rsNewBottom, rsNewTop
		}
		return r
	}
	f.ops.restackDry, f.ops.restackRun = rewritten(false), rewritten(true)
	useStackOps(t, f.ops)

	prevHeads, prevFollow := ascHeads, ascRestoreFollowUp
	ascHeads = f.heads
	ascRestoreFollowUp = func(_ context.Context, pr *issues_model.PullRequest, _ *user_model.User, oldID, newID string) {
		f.followUps = append(f.followUps, restackMove{pr.ID, oldID, newID})
	}
	t.Cleanup(func() { ascHeads, ascRestoreFollowUp = prevHeads, prevFollow })
	return f
}

func TestAscRestackStackMovesTheHeadsOfTheStack(t *testing.T) {
	f := newRestackFixture(t)
	doer, _ := ascMergePerm(t, f.top, 2)

	res, err := RestackAscStack(t.Context(), doer, f.top, AscRestackOptions{})
	require.NoError(t, err)

	// A dry run first (to authorise every rewritten change), then the real call, both for the stack of the change.
	require.Len(t, f.ops.restackReqs, 2)
	assert.True(t, f.ops.restackReqs[0].DryRun)
	assert.False(t, f.ops.restackReqs[1].DryRun)
	for _, r := range f.ops.restackReqs {
		assert.Equal(t, "user2/repo1", r.Repo)
		assert.Equal(t, ascMergeTop, r.Change)
		assert.False(t, r.AllowPartial, "one conflict or frozen change refuses the whole restack")
	}
	assert.True(t, res.Applied)
	assert.False(t, res.UpToDate)
	assert.Equal(t, rsOnto, res.Onto)
	assert.Equal(t, []restackMove{{f.bottom.ID, rsOldBottom, rsNewBottom}, {f.top.ID, rsOldTop, rsNewTop}}, f.heads.moves, "bottom first")
	assert.Equal(t, f.heads.moves, f.followUps, "each move runs what a push runs")
	require.Len(t, res.Moved, 2)
	require.Len(t, res.Items, 2)
	assert.Equal(t, f.bottom.ID, res.Items[0].PR.ID)
	assert.Equal(t, rsNewTop, res.Items[1].NewHead)
	assert.Equal(t, rsNewTop, res.Items[1].PR.HeadCommitID)
	assert.Empty(t, res.Failed)
	assert.True(t, res.Needs())
	assert.NoError(t, res.Blocker())
}

func TestAscRestackStackDryRunWritesNothing(t *testing.T) {
	f := newRestackFixture(t)
	doer, _ := ascMergePerm(t, f.top, 2)
	res, err := RestackAscStack(t.Context(), doer, f.top, AscRestackOptions{DryRun: true})
	require.NoError(t, err)
	require.Len(t, f.ops.restackReqs, 1)
	assert.True(t, f.ops.restackReqs[0].DryRun)
	assert.False(t, res.Applied)
	assert.Empty(t, f.heads.moves)
	assert.True(t, res.Needs())
	assert.Equal(t, f.top.ID, res.Items[1].PR.ID, "the preview names the pull requests")
}

func TestAscRestackStackUpToDate(t *testing.T) {
	f := newRestackFixture(t)
	doer, _ := ascMergePerm(t, f.top, 2)
	f.ops.restackDry = &ascbridge.RestackStackResult{Items: []ascbridge.RestackItem{
		{Change: ascMergeBottom, State: ascbridge.RestackSkippedLanded},
		{Change: ascMergeTop, State: ascbridge.RestackUnchanged, OldHead: rsOldTop},
	}}
	res, err := RestackAscStack(t.Context(), doer, f.top, AscRestackOptions{})
	require.NoError(t, err)
	assert.True(t, res.UpToDate)
	assert.False(t, res.Applied)
	assert.Len(t, f.ops.restackReqs, 1, "nothing to rewrite, so no write call")
	assert.Empty(t, f.heads.moves)
	assert.Nil(t, res.Items[0].PR, "a landed change has no open pull request")
}

// Security: a conflict, a blocked change or a frozen change stops the restack before anything is written.
func TestAscRestackStackBlockers(t *testing.T) {
	f := newRestackFixture(t)
	doer, _ := ascMergePerm(t, f.top, 2)

	f.ops.restackDry = &ascbridge.RestackStackResult{Items: []ascbridge.RestackItem{
		{Change: ascMergeBottom, State: ascbridge.RestackConflict, Conflicts: []string{"a.go"}},
		{Change: ascMergeTop, State: ascbridge.RestackBlocked},
	}}
	res, err := RestackAscStack(t.Context(), doer, f.top, AscRestackOptions{})
	require.ErrorIs(t, err, ascbridge.ErrStackConflict)
	var ce *ascbridge.ConflictError
	require.ErrorAs(t, err, &ce)
	assert.Equal(t, []string{"a.go"}, ce.Paths)
	require.NotNil(t, res, "the result still carries the preview")
	assert.Len(t, f.ops.restackReqs, 1)

	f.ops.restackDry = &ascbridge.RestackStackResult{Items: []ascbridge.RestackItem{
		{Change: ascMergeBottom, State: ascbridge.RestackSkippedFrozen},
		{Change: ascMergeTop, State: ascbridge.RestackRewritten},
	}}
	_, err = RestackAscStack(t.Context(), doer, f.top, AscRestackOptions{})
	require.ErrorIs(t, err, ascbridge.ErrStackFrozen)
	assert.Len(t, f.ops.restackReqs, 2, "no write call after the second dry run either")
	assert.Empty(t, f.heads.moves)

	// A dry run reports blockers without failing.
	res, err = RestackAscStack(t.Context(), doer, f.top, AscRestackOptions{DryRun: true})
	require.NoError(t, err)
	require.ErrorIs(t, res.Blocker(), ascbridge.ErrStackFrozen)
}

// Security: every change asc would rewrite needs an open pull request of this repository that the doer may update.
func TestAscRestackStackAuthorisation(t *testing.T) {
	f := newRestackFixture(t)

	// User 5 has no write access and is not the poster.
	reader, _ := ascMergePerm(t, f.top, 5)
	_, err := RestackAscStack(t.Context(), reader, f.top, AscRestackOptions{})
	require.ErrorIs(t, err, ErrAscRestackNotAllowed)
	assert.Empty(t, f.ops.restackReqs, "the doer may not rewrite the pull request itself: asc is not asked, not even for a dry run")
	_, err = RestackAscStack(t.Context(), reader, f.top, AscRestackOptions{DryRun: true})
	require.ErrorIs(t, err, ErrAscRestackNotAllowed)
	assert.Empty(t, f.ops.restackReqs)
	assert.Empty(t, f.heads.moves)

	doer, _ := ascMergePerm(t, f.top, 2)
	for _, bad := range []*user_model.User{nil, {ID: 2, IsActive: false}, {ID: 2, IsActive: true, ProhibitLogin: true}} {
		_, err = RestackAscStack(t.Context(), bad, f.top, AscRestackOptions{})
		require.ErrorIs(t, err, ErrAscRestackNotAllowed)
	}
	assert.Empty(t, f.ops.restackReqs)

	// A rewritten change without an open pull request in this repository is refused, whoever asks.
	f.ops.restackDry = &ascbridge.RestackStackResult{Items: []ascbridge.RestackItem{
		{Change: ascMergeBottom, State: ascbridge.RestackRewritten},
		{Change: ascMergeTop, State: ascbridge.RestackRewritten},
		{Change: rsOtherChg, State: ascbridge.RestackRewritten},
	}}
	_, err = RestackAscStack(t.Context(), doer, f.top, AscRestackOptions{})
	require.ErrorIs(t, err, ErrAscRestackNoPull)
	assert.Len(t, f.ops.restackReqs, 1, "only the dry run reached asc")

	// ...but an unchanged one without a pull request is only reported, as long as asc leaves it alone.
	f.ops.restackDry = &ascbridge.RestackStackResult{Items: []ascbridge.RestackItem{
		{Change: ascMergeBottom, State: ascbridge.RestackRewritten, OldHead: rsOldBottom, OldHeadID: rsIDBottom},
		{Change: ascMergeTop, State: ascbridge.RestackRewritten, OldHead: rsOldTop, OldHeadID: rsIDTop},
		{Change: rsOtherChg, State: ascbridge.RestackUnchanged},
	}}
	f.ops.restackRun = &ascbridge.RestackStackResult{Applied: true, Items: []ascbridge.RestackItem{
		{Change: ascMergeBottom, State: ascbridge.RestackRewritten, OldHead: rsOldBottom, OldHeadID: rsIDBottom, NewHead: rsNewBottom},
		{Change: ascMergeTop, State: ascbridge.RestackRewritten, OldHead: rsOldTop, OldHeadID: rsIDTop, NewHead: rsNewTop},
		{Change: rsOtherChg, State: ascbridge.RestackUnchanged},
	}}
	res, err := RestackAscStack(t.Context(), doer, f.top, AscRestackOptions{})
	require.NoError(t, err)
	assert.Len(t, res.Moved, 2, "a head is moved only for a pull request that was checked")
}

// Security: the apply is pinned to the preview that was authorised.
func TestAscRestackStackPinsTheApplyToThePreview(t *testing.T) {
	f := newRestackFixture(t)
	doer, _ := ascMergePerm(t, f.top, 2)

	_, err := RestackAscStack(t.Context(), doer, f.top, AscRestackOptions{})
	require.NoError(t, err)
	require.Len(t, f.ops.restackReqs, 2)
	assert.Empty(t, f.ops.restackReqs[0].ExpectedHeads, "the preview is not pinned")
	assert.Equal(t, []ascbridge.ExpectedHead{
		{Change: ascMergeBottom, OldHeadID: rsIDBottom},
		{Change: ascMergeTop, OldHeadID: rsIDTop},
	}, f.ops.restackReqs[1].ExpectedHeads, "the apply carries the head the preview showed for each change it may rewrite")
}

func TestAscRestackStackPinsPatchIdenticalChanges(t *testing.T) {
	f := newRestackFixture(t)
	doer, _ := ascMergePerm(t, f.top, 2)
	// asc may report a dry run as unchanged and still write a new commit on the apply: the pin lists it too.
	f.ops.restackDry.Items[1].State = ascbridge.RestackUnchanged
	f.ops.restackRun.Items[1].State = ascbridge.RestackUnchanged
	_, err := RestackAscStack(t.Context(), doer, f.top, AscRestackOptions{})
	require.NoError(t, err)
	assert.Len(t, f.ops.restackReqs[1].ExpectedHeads, 2)
}

func TestAscRestackStackRefusesAChangeWithoutAHeadToPin(t *testing.T) {
	f := newRestackFixture(t)
	doer, _ := ascMergePerm(t, f.top, 2)
	f.ops.restackDry.Items[1].OldHeadID = ""
	_, err := RestackAscStack(t.Context(), doer, f.top, AscRestackOptions{})
	require.ErrorIs(t, err, ErrAscRestackNoPin)
	assert.Len(t, f.ops.restackReqs, 1, "an unpinned apply is never sent")
	assert.Empty(t, f.heads.moves)
}

// Security: whatever asc answers, a head moves only for a change the authorised preview listed, with the head
// the preview showed. Otherwise the call fails loudly and nothing moves.
func TestAscRestackStackVerifiesWhatAscRewrote(t *testing.T) {
	doerFor := func(t *testing.T, f *restackFixture) *user_model.User {
		t.Helper()
		d, _ := ascMergePerm(t, f.top, 2)
		return d
	}
	t.Run("a change that was not in the preview", func(t *testing.T) {
		f := newRestackFixture(t)
		f.ops.restackRun.Items = append(f.ops.restackRun.Items,
			ascbridge.RestackItem{Change: rsOtherChg, State: ascbridge.RestackRewritten, OldHead: rsOldTop, NewHead: rsOnto})
		res, err := RestackAscStack(t.Context(), doerFor(t, f), f.top, AscRestackOptions{})
		require.ErrorIs(t, err, ErrAscRestackNotPreviewed)
		assert.Contains(t, err.Error(), "no pull request head was moved")
		require.NotNil(t, res)
		assert.Empty(t, f.heads.moves, "not even the authorised changes move")
		assert.Empty(t, f.followUps)
		assert.Empty(t, res.Moved)
	})
	t.Run("a change without an open pull request that the preview called unchanged", func(t *testing.T) {
		f := newRestackFixture(t)
		f.ops.restackDry.Items = append(f.ops.restackDry.Items, ascbridge.RestackItem{Change: rsOtherChg, State: ascbridge.RestackUnchanged})
		f.ops.restackRun.Items = append(f.ops.restackRun.Items,
			ascbridge.RestackItem{Change: rsOtherChg, State: ascbridge.RestackRewritten, NewHead: rsOnto})
		_, err := RestackAscStack(t.Context(), doerFor(t, f), f.top, AscRestackOptions{})
		require.ErrorIs(t, err, ErrAscRestackNotPreviewed)
		assert.Empty(t, f.heads.moves)
	})
	t.Run("an unchanged change that carries a new head but has no checked pull request", func(t *testing.T) {
		f := newRestackFixture(t)
		f.ops.restackDry.Items = append(f.ops.restackDry.Items, ascbridge.RestackItem{Change: rsOtherChg, State: ascbridge.RestackUnchanged})
		f.ops.restackRun.Items = append(f.ops.restackRun.Items,
			ascbridge.RestackItem{Change: rsOtherChg, State: ascbridge.RestackUnchanged, OldHead: rsOldTop, NewHead: rsOnto})
		_, err := RestackAscStack(t.Context(), doerFor(t, f), f.top, AscRestackOptions{})
		require.ErrorIs(t, err, ErrAscRestackNotPreviewed)
		assert.Empty(t, f.heads.moves)
	})
	t.Run("another old head than the preview showed", func(t *testing.T) {
		f := newRestackFixture(t)
		f.ops.restackRun.Items[1].OldHeadID = "3333333333333333333333333333333333333333333333333333333333333333"
		_, err := RestackAscStack(t.Context(), doerFor(t, f), f.top, AscRestackOptions{})
		require.ErrorIs(t, err, ErrAscRestackNotPreviewed)
		assert.Empty(t, f.heads.moves)

		f = newRestackFixture(t)
		f.ops.restackRun.Items[0].OldHead = rsOnto
		_, err = RestackAscStack(t.Context(), doerFor(t, f), f.top, AscRestackOptions{})
		require.ErrorIs(t, err, ErrAscRestackNotPreviewed)
		assert.Empty(t, f.heads.moves)
	})
	t.Run("a change the preview called landed", func(t *testing.T) {
		f := newRestackFixture(t)
		f.ops.restackRun.Items = append(f.ops.restackRun.Items,
			ascbridge.RestackItem{Change: ascMergeBottom, State: ascbridge.RestackRewritten, OldHead: rsOldBottom, NewHead: rsOnto})
		f.ops.restackDry.Items[0].State = ascbridge.RestackSkippedLanded
		_, err := RestackAscStack(t.Context(), doerFor(t, f), f.top, AscRestackOptions{})
		require.ErrorIs(t, err, ErrAscRestackNotPreviewed)
		assert.Empty(t, f.heads.moves)
	})
	t.Run("the authorised answer passes", func(t *testing.T) {
		f := newRestackFixture(t)
		_, err := RestackAscStack(t.Context(), doerFor(t, f), f.top, AscRestackOptions{})
		require.NoError(t, err)
		assert.Len(t, f.heads.moves, 2)
	})
}

// A pull request that was pushed to after asc read it keeps its head.
func TestAscRestackStackDoesNotMoveAHeadThatChanged(t *testing.T) {
	f := newRestackFixture(t)
	doer, _ := ascMergePerm(t, f.top, 2)
	f.heads.heads[f.top.ID] = rsOnto // pushed since
	res, err := RestackAscStack(t.Context(), doer, f.top, AscRestackOptions{})
	require.Error(t, err)
	assert.Len(t, res.Moved, 1)
	require.Len(t, res.Failed, 1)
	assert.Equal(t, rsOnto, f.heads.heads[f.top.ID])
}

// Restacks of one stack run one at a time.
func TestAscRestackLocksPerStack(t *testing.T) {
	ctx := t.Context()
	unlock, err := lockAscRestack(ctx, "1/10")
	require.NoError(t, err)

	got := make(chan func(), 1)
	go func() {
		u, err := lockAscRestack(ctx, "1/10")
		if err == nil {
			got <- u
		}
	}()
	select {
	case <-got:
		t.Fatal("a second restack of the same stack ran at the same time")
	case <-time.After(50 * time.Millisecond):
	}

	// another stack is not held up
	other, err := lockAscRestack(ctx, "1/11")
	require.NoError(t, err)
	other()

	// a waiter gives up with its context
	short, cancel := context.WithTimeout(ctx, 20*time.Millisecond)
	defer cancel()
	_, err = lockAscRestack(short, "1/10")
	require.ErrorIs(t, err, context.DeadlineExceeded)

	unlock()
	select {
	case u := <-got:
		u()
	case <-time.After(2 * time.Second):
		t.Fatal("the waiter did not get the lock")
	}
	ascRestackLocks.Lock()
	defer ascRestackLocks.Unlock()
	assert.Empty(t, ascRestackLocks.m, "locks nobody holds or waits for are dropped")
}

func TestAscRestackStackWaitsForTheStack(t *testing.T) {
	f := newRestackFixture(t)
	doer, _ := ascMergePerm(t, f.top, 2)
	row, err := issues_model.GetPullRequestAscStack(t.Context(), f.top.ID)
	require.NoError(t, err)
	key, err := ascRestackKey(t.Context(), f.top, row)
	require.NoError(t, err)
	bottomRow, err := issues_model.GetPullRequestAscStack(t.Context(), f.bottom.ID)
	require.NoError(t, err)
	bottomKey, err := ascRestackKey(t.Context(), f.bottom, bottomRow)
	require.NoError(t, err)
	assert.Equal(t, bottomKey, key, "every pull request of a stack has the key of its bottom")

	unlock, err := lockAscRestack(t.Context(), key)
	require.NoError(t, err)
	defer unlock()
	short, cancel := context.WithTimeout(t.Context(), 300*time.Millisecond)
	defer cancel()
	_, err = RestackAscStack(short, doer, f.bottom, AscRestackOptions{})
	require.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Empty(t, f.ops.restackReqs, "asc was not asked while the stack was being restacked")

	// a dry run writes nothing and does not wait
	_, err = RestackAscStack(t.Context(), doer, f.bottom, AscRestackOptions{DryRun: true})
	require.NoError(t, err)
}

// Only an approval that is for exactly the old head, live and not dismissed, moves to the new head.
func TestKeepAscApprovalsMovesOnlyTheExactLiveOnes(t *testing.T) {
	f := newRestackFixture(t)
	e := db.GetEngine(t.Context())
	add := func(rev issues_model.Review) *issues_model.Review {
		rev.IssueID, rev.ReviewerID = f.top.IssueID, 1
		_, err := e.Insert(&rev)
		require.NoError(t, err)
		return &rev
	}
	live := add(issues_model.Review{Type: issues_model.ReviewTypeApprove, CommitID: rsOldTop})
	stale := add(issues_model.Review{Type: issues_model.ReviewTypeApprove, CommitID: rsOldTop, Stale: true})
	dismissed := add(issues_model.Review{Type: issues_model.ReviewTypeApprove, CommitID: rsOldTop, Dismissed: true})
	older := add(issues_model.Review{Type: issues_model.ReviewTypeApprove, CommitID: rsOldBottom})
	reject := add(issues_model.Review{Type: issues_model.ReviewTypeReject, CommitID: rsOldTop})
	otherIssue := add(issues_model.Review{Type: issues_model.ReviewTypeApprove, CommitID: rsOldTop})
	_, err := e.Exec("UPDATE `review` SET issue_id=? WHERE id=?", f.bottom.IssueID, otherIssue.ID)
	require.NoError(t, err)

	require.NoError(t, keepAscApprovals(t.Context(), f.top, rsOldTop, rsNewTop))

	get := func(r *issues_model.Review) *issues_model.Review {
		return unittest.AssertExistsAndLoadBean(t, &issues_model.Review{ID: r.ID})
	}
	assert.Equal(t, rsNewTop, get(live).CommitID)
	assert.False(t, get(live).Stale)
	assert.Equal(t, rsOldTop, get(stale).CommitID, "a stale approval does not come back")
	assert.True(t, get(stale).Stale, "and is never reset")
	assert.Equal(t, rsOldTop, get(dismissed).CommitID)
	assert.True(t, get(dismissed).Dismissed)
	assert.Equal(t, rsOldBottom, get(older).CommitID, "an approval of another commit is not carried")
	assert.Equal(t, rsOldTop, get(reject).CommitID, "only approvals move")
	assert.Equal(t, rsOldTop, get(otherIssue).CommitID, "only the pull request's own reviews move")

	// nothing to carry without both heads
	require.NoError(t, keepAscApprovals(t.Context(), f.top, "", rsNewTop))
	require.NoError(t, keepAscApprovals(t.Context(), f.top, rsOldTop, ""))
	assert.Equal(t, rsOldTop, get(stale).CommitID)
	assert.Equal(t, rsNewTop, get(live).CommitID)
}

func TestAscRestackStackRefusals(t *testing.T) {
	f := newRestackFixture(t)
	doer, _ := ascMergePerm(t, f.top, 2)

	lone := unittest.AssertExistsAndLoadBean(t, &issues_model.PullRequest{ID: 3})
	_, err := RestackAscStack(t.Context(), doer, lone, AscRestackOptions{})
	require.ErrorIs(t, err, ErrAscStackNotStacked)
	_, err = RestackAscStack(t.Context(), doer, nil, AscRestackOptions{})
	require.ErrorIs(t, err, ErrAscStackNotStacked)

	prev := ascStackBacked
	ascStackBacked = func(*repo_model.Repository) (bool, error) { return false, nil }
	_, err = RestackAscStack(t.Context(), doer, f.top, AscRestackOptions{})
	require.ErrorIs(t, err, ErrAscStackDisabled)
	ascStackBacked = prev

	// A merged pull request is not restacked.
	merged := *f.bottom
	merged.HasMerged = true
	_, err = RestackAscStack(t.Context(), doer, &merged, AscRestackOptions{})
	require.ErrorIs(t, err, ErrAscRestackNotOpen)
	_, err = db.GetEngine(t.Context()).Exec("UPDATE `issue` SET is_closed=? WHERE id=?", true, f.top.IssueID)
	require.NoError(t, err)
	closed := unittest.AssertExistsAndLoadBean(t, &issues_model.PullRequest{ID: f.top.ID})
	_, err = RestackAscStack(t.Context(), doer, closed, AscRestackOptions{})
	require.ErrorIs(t, err, ErrAscRestackNotOpen)
	assert.Empty(t, f.ops.restackReqs)
}

func TestAscRestackStackAscErrorsChangeNothing(t *testing.T) {
	f := newRestackFixture(t)
	doer, _ := ascMergePerm(t, f.top, 2)

	f.ops.dryErr = ascbridge.ErrStacksUnavailable
	_, err := RestackAscStack(t.Context(), doer, f.top, AscRestackOptions{})
	require.ErrorIs(t, err, ascbridge.ErrStacksUnavailable)
	f.ops.dryErr = nil

	f.ops.restackErr = ascbridge.ErrStackFrozen
	_, err = RestackAscStack(t.Context(), doer, f.top, AscRestackOptions{})
	require.ErrorIs(t, err, ascbridge.ErrStackFrozen)
	assert.Empty(t, f.heads.moves, "when asc refuses the write, no head moves")
}

func TestAscRestackStackReportsHeadsThatDidNotMove(t *testing.T) {
	f := newRestackFixture(t)
	doer, _ := ascMergePerm(t, f.top, 2)
	f.heads.moveErr[f.top.ID] = errors.New("ref locked")

	res, err := RestackAscStack(t.Context(), doer, f.top, AscRestackOptions{})
	require.Error(t, err)
	require.NotNil(t, res)
	assert.Contains(t, err.Error(), "asc restacked the stack")
	assert.Len(t, res.Moved, 1, "the bottom moved")
	require.Len(t, res.Failed, 1)
	assert.Equal(t, f.top.ID, res.Failed[0].ID)
}

func TestAscRestackStackKeepsApprovalsOfAPatchIdenticalRewrite(t *testing.T) {
	f := newRestackFixture(t)
	doer, _ := ascMergePerm(t, f.top, 2)
	rev := &issues_model.Review{IssueID: f.top.IssueID, ReviewerID: 1, Type: issues_model.ReviewTypeApprove, CommitID: rsOldTop}
	_, err := db.GetEngine(t.Context()).Insert(rev)
	require.NoError(t, err)
	other := &issues_model.Review{IssueID: f.bottom.IssueID, ReviewerID: 1, Type: issues_model.ReviewTypeApprove, CommitID: rsOldBottom}
	_, err = db.GetEngine(t.Context()).Insert(other)
	require.NoError(t, err)

	// The bottom is rewritten with a changed patch; the top is only moved onto it with the same patch.
	f.ops.restackDry.Items[1].State = ascbridge.RestackRewritten
	f.ops.restackRun.Items[1].State = ascbridge.RestackUnchanged
	_, err = RestackAscStack(t.Context(), doer, f.top, AscRestackOptions{})
	require.NoError(t, err)

	got := unittest.AssertExistsAndLoadBean(t, &issues_model.Review{ID: rev.ID})
	assert.Equal(t, rsNewTop, got.CommitID, "the approval follows the head where the patch is the same")
	assert.False(t, got.Stale)
	kept := unittest.AssertExistsAndLoadBean(t, &issues_model.Review{ID: other.ID})
	assert.Equal(t, rsOldBottom, kept.CommitID, "a changed patch does not keep the approval")
}

func TestAscRestackStackNoBackend(t *testing.T) {
	f := newRestackFixture(t)
	doer, _ := ascMergePerm(t, f.top, 2)
	// With no internal token and no fake the real client cannot be built: fail closed, nothing is written.
	t.Cleanup(ascbridge.UseStackOps(nil))
	_, err := RestackAscStack(t.Context(), doer, f.top, AscRestackOptions{})
	require.ErrorIs(t, err, ErrAscStackDisabled)
}
