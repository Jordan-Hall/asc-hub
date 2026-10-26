// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package pull

import (
	"context"
	"errors"
	"testing"

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
			{Change: ascMergeBottom, State: ascbridge.RestackRewritten, OldHead: rsOldBottom, NewVersion: 2},
			{Change: ascMergeTop, State: ascbridge.RestackRewritten, OldHead: rsOldTop, NewVersion: 2},
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
	assert.Len(t, f.ops.restackReqs, 1, "only the dry run reached asc")
	assert.Empty(t, f.heads.moves)

	doer, _ := ascMergePerm(t, f.top, 2)
	for _, bad := range []*user_model.User{nil, {ID: 2, IsActive: false}, {ID: 2, IsActive: true, ProhibitLogin: true}} {
		_, err = RestackAscStack(t.Context(), bad, f.top, AscRestackOptions{})
		require.ErrorIs(t, err, ErrAscRestackNotAllowed)
	}
	assert.Len(t, f.ops.restackReqs, 1)

	// A rewritten change without an open pull request in this repository is refused, whoever asks.
	f.ops.restackDry = &ascbridge.RestackStackResult{Items: []ascbridge.RestackItem{
		{Change: ascMergeBottom, State: ascbridge.RestackRewritten},
		{Change: ascMergeTop, State: ascbridge.RestackRewritten},
		{Change: rsOtherChg, State: ascbridge.RestackRewritten},
	}}
	_, err = RestackAscStack(t.Context(), doer, f.top, AscRestackOptions{})
	require.ErrorIs(t, err, ErrAscRestackNoPull)
	assert.Len(t, f.ops.restackReqs, 2)

	// ...but an unchanged one without a pull request is only reported.
	f.ops.restackDry.Items[2].State = ascbridge.RestackUnchanged
	f.ops.restackRun = &ascbridge.RestackStackResult{Applied: true, Items: []ascbridge.RestackItem{
		{Change: ascMergeBottom, State: ascbridge.RestackRewritten, NewHead: rsNewBottom},
		{Change: ascMergeTop, State: ascbridge.RestackRewritten, NewHead: rsNewTop},
		{Change: rsOtherChg, State: ascbridge.RestackRewritten, NewHead: rsOnto},
	}}
	res, err := RestackAscStack(t.Context(), doer, f.top, AscRestackOptions{})
	require.NoError(t, err)
	assert.Len(t, res.Moved, 2, "a head is moved only for a pull request that was checked")
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
