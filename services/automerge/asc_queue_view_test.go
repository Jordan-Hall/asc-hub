// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package automerge

import (
	"context"
	"errors"
	"testing"

	"forgejo.org/models/db"
	issues_model "forgejo.org/models/issues"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/services/ascbridge"
	pull_service "forgejo.org/services/pull"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeQStackOps answers the dry run of a restack. Only RestackStack is used here.
type fakeQStackOps struct {
	dry    *ascbridge.RestackStackResult
	dryErr error
	calls  int
}

func (f *fakeQStackOps) RestackStack(context.Context, ascbridge.RestackStackRequest) (*ascbridge.RestackStackResult, error) {
	f.calls++
	return f.dry, f.dryErr
}

func (f *fakeQStackOps) RevertLanded(context.Context, ascbridge.RevertLandedRequest) (*ascbridge.RevertLandedResult, error) {
	return nil, errors.New("not used")
}

// dryRunFixture makes the heads of the stack resolve and the restack preview answer.
func dryRunFixture(t *testing.T) (*qFixture, *fakeQStackOps) {
	t.Helper()
	f := newQFixture(t)
	t.Cleanup(pull_service.UseAscStackHead(func(_ context.Context, pr *issues_model.PullRequest) (string, error) {
		return f.heads[pr.ID], nil
	}))
	t.Cleanup(pull_service.UseAscStackBacked(func(*repo_model.Repository) (bool, error) { return true, nil }))
	ops := &fakeQStackOps{dry: &ascbridge.RestackStackResult{Items: []ascbridge.RestackItem{
		{Change: qBottomChange, State: ascbridge.RestackUnchanged}, {Change: qTopChange, State: ascbridge.RestackUnchanged},
	}}}
	t.Cleanup(ascbridge.UseStackOps(ops))
	return f, ops
}

func TestAscQueueViewShowsPositionsPausedAndRecent(t *testing.T) {
	f := newQFixture(t)
	owner, other, siteAdmin := qUser(t, 2), qUser(t, 5), qUser(t, 1)
	require.NoError(t, f.bottom.LoadBaseRepo(t.Context()))
	repo := f.bottom.BaseRepo

	_, err := AscQueueEnqueue(t.Context(), owner, f.top)
	require.NoError(t, err)

	v, err := AscQueueViewOf(t.Context(), repo, "master", owner)
	require.NoError(t, err)
	assert.Equal(t, "master", v.Branch)
	assert.False(t, v.Paused)
	assert.False(t, v.Landing)
	require.Len(t, v.Active, 2)
	assert.Equal(t, 1, v.Active[0].Position)
	assert.Equal(t, 2, v.Active[1].Position)
	assert.Equal(t, f.bottom.ID, v.Active[0].PR.ID)
	require.NotNil(t, v.Active[0].PR.Issue, "the issue is loaded for the title")
	require.NotNil(t, v.Active[0].PR.Issue.Repo, "and its repository, which the link needs")
	assert.NotEmpty(t, v.Active[0].PR.Issue.Link())
	assert.True(t, v.Active[0].CanCancel, "the user who queued it")
	assert.Empty(t, v.Recent)

	// Another user sees the queue and may not remove an entry that is not theirs; a site admin may.
	v, err = AscQueueViewOf(t.Context(), repo, "master", other)
	require.NoError(t, err)
	assert.False(t, v.Active[0].CanCancel)
	v, err = AscQueueViewOf(t.Context(), repo, "master", siteAdmin)
	require.NoError(t, err)
	assert.True(t, v.Active[0].CanCancel)
	v, err = AscQueueViewOf(t.Context(), repo, "master", nil)
	require.NoError(t, err)
	assert.False(t, v.Active[0].CanCancel, "a visitor may only look")

	require.NoError(t, AscQueueSetPaused(t.Context(), owner, repo, "master", true))
	v, err = AscQueueViewOf(t.Context(), repo, "master", owner)
	require.NoError(t, err)
	assert.True(t, v.Paused)

	// A lease that is held shows as landing; a lease that ran out does not.
	require.NoError(t, AscQueueSetPaused(t.Context(), owner, repo, "master", false))
	unlock, ok, err := ascQueueLock(t.Context(), repo.ID, "master")
	require.NoError(t, err)
	require.True(t, ok)
	v, err = AscQueueViewOf(t.Context(), repo, "master", owner)
	require.NoError(t, err)
	assert.True(t, v.Landing)
	unlock()

	// After the run both landed: they leave Active and show as finished.
	n, err := RunAscStackQueue(t.Context(), repo.ID, "master")
	require.NoError(t, err)
	assert.Equal(t, 2, n)
	v, err = AscQueueViewOf(t.Context(), repo, "master", owner)
	require.NoError(t, err)
	assert.Empty(t, v.Active)
	require.Len(t, v.Recent, 2)
	assert.Equal(t, AscQueueMerged, v.Recent[0].Entry.State)

	// Another branch has its own, empty, queue.
	v, err = AscQueueViewOf(t.Context(), repo, "other", owner)
	require.NoError(t, err)
	assert.Empty(t, v.Active)
	assert.Empty(t, v.Recent)
}

func TestAscQueueViewRecentIsBoundedAndSurvivesAGonePullRequest(t *testing.T) {
	f := newQFixture(t)
	owner := qUser(t, 2)
	require.NoError(t, f.bottom.LoadBaseRepo(t.Context()))
	for i := range AscQueueRecentLimit + 5 {
		_, err := db.GetEngine(t.Context()).Insert(&AscStackQueueEntry{
			RepoID: f.bottom.BaseRepoID, Branch: "master", PullID: 1000 + int64(i), AscChange: qBottomChange, DoerID: 2, State: AscQueueCancelled,
		})
		require.NoError(t, err)
	}
	v, err := AscQueueViewOf(t.Context(), f.bottom.BaseRepo, "master", owner)
	require.NoError(t, err)
	assert.Len(t, v.Recent, AscQueueRecentLimit)
	for _, r := range v.Recent {
		assert.Nil(t, r.PR, "a pull request that is gone shows without a title")
	}
}

func TestAscQueueEntriesByPull(t *testing.T) {
	f := newQFixture(t)
	got, err := AscQueueEntries(t.Context(), nil)
	require.NoError(t, err)
	assert.Empty(t, got)

	_, err = AscQueueEnqueue(t.Context(), qUser(t, 2), f.bottom)
	require.NoError(t, err)
	got, err = AscQueueEntries(t.Context(), []int64{f.bottom.ID, f.top.ID})
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, AscQueueQueued, got[f.bottom.ID].State)
}

func TestAscQueueMayAdmin(t *testing.T) {
	f := newQFixture(t)
	require.NoError(t, f.bottom.LoadBaseRepo(t.Context()))
	repo := f.bottom.BaseRepo
	for id, want := range map[int64]bool{1: true, 2: true, 5: false} {
		got, err := AscQueueMayAdmin(t.Context(), qUser(t, id), repo)
		require.NoError(t, err)
		assert.Equal(t, want, got, "user %d", id)
	}
	got, err := AscQueueMayAdmin(t.Context(), nil, repo)
	require.NoError(t, err)
	assert.False(t, got)
}

func TestAscQueueDryRunReady(t *testing.T) {
	f, ops := dryRunFixture(t)
	d, err := AscQueueDryRunOf(t.Context(), qUser(t, 2), f.top)
	require.NoError(t, err)
	require.Len(t, d.Checks, 2)
	for _, c := range d.Checks {
		require.NoError(t, c.Err)
	}
	assert.True(t, d.Ready)
	assert.False(t, d.Paused)
	assert.Empty(t, d.Queued)
	require.NoError(t, d.RestackErr)
	assert.False(t, d.NeedsContentMerge, "the stack is on the target")
	assert.True(t, d.NoContentMerge)
	assert.Equal(t, 1, ops.calls)

	// The dry run wrote nothing: the queue is empty and nothing is merged.
	list, err := AscQueueList(t.Context(), f.top.BaseRepoID, "master")
	require.NoError(t, err)
	assert.Empty(t, list)
	assert.Empty(t, f.lander.lands)
}

func TestAscQueueDryRunNotReadyPausedAndQueued(t *testing.T) {
	f, ops := dryRunFixture(t)
	doer := qUser(t, 2)

	// A head that moved since the check.
	f.top.HeadCommitID = "ffffffffffffffffffffffffffffffffffffffff"
	d, err := AscQueueDryRunOf(t.Context(), doer, f.top)
	require.NoError(t, err)
	assert.False(t, d.Ready)
	require.NoError(t, d.Checks[0].Err)
	require.ErrorIs(t, d.Checks[1].Err, pull_service.ErrAscStackHeadMoved)

	// Queued and paused are reported.
	f.top.HeadCommitID = ""
	_, err = AscQueueEnqueue(t.Context(), doer, f.top)
	require.NoError(t, err)
	require.NoError(t, f.top.LoadBaseRepo(t.Context()))
	require.NoError(t, AscQueueSetPaused(t.Context(), doer, f.top.BaseRepo, "master", true))
	d, err = AscQueueDryRunOf(t.Context(), doer, f.top)
	require.NoError(t, err)
	assert.True(t, d.Paused)
	assert.Len(t, d.Queued, 2)
	assert.Equal(t, 1+1, ops.calls)
}

func TestAscQueueDryRunFindsAContentMerge(t *testing.T) {
	f, ops := dryRunFixture(t)
	ops.dry = &ascbridge.RestackStackResult{Items: []ascbridge.RestackItem{
		{Change: qBottomChange, State: ascbridge.RestackRewritten}, {Change: qTopChange, State: ascbridge.RestackRewritten},
	}}
	d, err := AscQueueDryRunOf(t.Context(), qUser(t, 2), f.top)
	require.NoError(t, err)
	assert.True(t, d.NeedsContentMerge, "a land now would have to merge file contents")

	// With the setting off the page still reports it; the caller decides what to say.
	ascQueueRevalidateSetting = func() string { return "off" }
	d, err = AscQueueDryRunOf(t.Context(), qUser(t, 2), f.top)
	require.NoError(t, err)
	assert.False(t, d.NoContentMerge)
	assert.True(t, d.NeedsContentMerge)

	// A restack preview that fails does not fail the dry run of the merge.
	ops.dry, ops.dryErr = nil, ascbridge.ErrStacksUnavailable
	d, err = AscQueueDryRunOf(t.Context(), qUser(t, 2), f.top)
	require.NoError(t, err)
	require.ErrorIs(t, d.RestackErr, ascbridge.ErrStacksUnavailable)
	assert.False(t, d.NeedsContentMerge)
	assert.True(t, d.Ready)
}

func TestAscQueueDryRunRefusals(t *testing.T) {
	f, _ := dryRunFixture(t)
	_, err := AscQueueDryRunOf(t.Context(), nil, f.top)
	require.ErrorIs(t, err, ErrAscQueueNotAllowed)
	prev := ascQueueBacked
	ascQueueBacked = func(*repo_model.Repository) (bool, error) { return false, nil }
	t.Cleanup(func() { ascQueueBacked = prev })
	_, err = AscQueueDryRunOf(t.Context(), qUser(t, 2), f.top)
	require.ErrorIs(t, err, ErrAscQueueDisabled)

	ascQueueBacked = prev
	lone := qLonePull(t)
	_, err = AscQueueDryRunOf(t.Context(), qUser(t, 2), lone)
	require.ErrorIs(t, err, pull_service.ErrAscStackNotStacked)
}

// qLonePull is a pull request that is not part of a stack.
func qLonePull(t *testing.T) *issues_model.PullRequest {
	t.Helper()
	pr := &issues_model.PullRequest{}
	has, err := db.GetEngine(t.Context()).ID(3).Get(pr)
	require.NoError(t, err)
	require.True(t, has)
	return pr
}
