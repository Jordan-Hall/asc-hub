// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package automerge

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"forgejo.org/models/db"
	issues_model "forgejo.org/models/issues"
	access_model "forgejo.org/models/perm/access"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"
	pull_service "forgejo.org/services/pull"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	qBottomChange = "kkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkk"
	qTopChange    = "llllllllllllllllllllllllllllllll"
)

// fakeLander lands like pull_service.MergeAscStack would: it refuses a prefix with a pull request that is not
// ready, and marks the prefix merged.
type fakeLander struct {
	t          *testing.T
	notReady   map[int64]bool
	needsCM    bool
	restackErr error
	lands      []int64 // pull id asked, in order
	flags      []bool  // noContentMerge of each land
	restacks   int
	inflight   int
	maxFlight  int
}

func (f *fakeLander) Land(ctx context.Context, _ *user_model.User, _ access_model.Permission, pr *issues_model.PullRequest, noCM bool) (*pull_service.AscStackMergeResult, error) {
	f.inflight++
	f.maxFlight = max(f.maxFlight, f.inflight)
	defer func() { f.inflight-- }()
	f.lands = append(f.lands, pr.ID)
	f.flags = append(f.flags, noCM)
	items, err := pull_service.AscStackPrefix(ctx, pr)
	if err != nil {
		return nil, err
	}
	for _, it := range items {
		if f.notReady[it.PR.ID] {
			return nil, fmt.Errorf("%w: #%d", pull_service.ErrAscStackNotMergable, it.PR.Index)
		}
	}
	if f.needsCM {
		return nil, ErrAscLandNeedsContentMerge
	}
	res := &pull_service.AscStackMergeResult{}
	for _, it := range items {
		_, err := db.GetEngine(ctx).ID(it.PR.ID).Cols("has_merged").Update(&issues_model.PullRequest{HasMerged: true})
		require.NoError(f.t, err)
		res.Merged = append(res.Merged, it.PR)
	}
	return res, nil
}

func (f *fakeLander) Restack(context.Context, *user_model.User, *issues_model.PullRequest) error {
	f.restacks++
	return f.restackErr
}

type qFixture struct {
	bottom, top *issues_model.PullRequest
	lander      *fakeLander
	heads       map[int64]string
}

func allowAll(context.Context, *issues_model.PullRequest, access_model.Permission, *user_model.User) (bool, error) {
	return true, nil
}

func newQFixture(t *testing.T) *qFixture {
	t.Helper()
	require.NoError(t, unittest.PrepareTestDatabase())
	ctx := t.Context()
	f := &qFixture{lander: &fakeLander{t: t, notReady: map[int64]bool{}}}
	f.bottom = unittest.AssertExistsAndLoadBean(t, &issues_model.PullRequest{ID: 2})
	f.top = unittest.AssertExistsAndLoadBean(t, &issues_model.PullRequest{ID: 5})
	f.top.BaseBranch = "master"
	_, err := db.GetEngine(ctx).ID(f.top.ID).Cols("base_branch").Update(f.top)
	require.NoError(t, err)
	require.NoError(t, issues_model.SetPullRequestAscStack(ctx, f.bottom, qBottomChange, 0))
	require.NoError(t, issues_model.SetPullRequestAscStack(ctx, f.top, qTopChange, f.bottom.ID))

	f.heads = map[int64]string{f.bottom.ID: "aaaa", f.top.ID: "bbbb"}
	prevHead, prevAllowed, prevBacked, prevRev := ascQueueHead, ascQueueMayMerge, ascQueueBacked, ascQueueRevalidateSetting
	ascQueueHead = func(_ context.Context, pr *issues_model.PullRequest) (string, error) { return f.heads[pr.ID], nil }
	ascQueueMayMerge = allowAll
	ascQueueBacked = func(*repo_model.Repository) (bool, error) { return true, nil }
	ascQueueRevalidateSetting = func() string { return "" }
	restoreLander := UseAscQueueLander(f.lander)
	restoreKick := UseAscQueueKicker(func(int64, string) {})
	t.Cleanup(func() {
		ascQueueHead, ascQueueMayMerge, ascQueueBacked, ascQueueRevalidateSetting = prevHead, prevAllowed, prevBacked, prevRev
		restoreLander()
		restoreKick()
	})
	return f
}

func qUser(t *testing.T, id int64) *user_model.User {
	t.Helper()
	return unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: id})
}

func qStates(t *testing.T, f *qFixture) map[int64]string {
	t.Helper()
	list, err := AscQueueList(t.Context(), f.bottom.BaseRepoID, "master")
	require.NoError(t, err)
	out := map[int64]string{}
	for _, e := range list {
		out[e.PullID] = e.State
	}
	return out
}

func TestAscStackQueueEnqueueEnablesPrefixInOrder(t *testing.T) {
	f := newQFixture(t)
	res, err := AscQueueEnqueue(t.Context(), qUser(t, 2), f.top)
	require.NoError(t, err)
	require.Len(t, res.Queued, 2)
	assert.Equal(t, qBottomChange, res.Queued[0].AscChange)
	assert.Equal(t, qTopChange, res.Queued[1].AscChange)

	// A second request changes nothing and keeps the places.
	again, err := AscQueueEnqueue(t.Context(), qUser(t, 2), f.top)
	require.NoError(t, err)
	require.Len(t, again.Queued, 2)
	assert.Equal(t, res.Queued[0].ID, again.Queued[0].ID)
	list, err := AscQueueList(t.Context(), f.top.BaseRepoID, "master")
	require.NoError(t, err)
	assert.Len(t, list, 2)
}

func TestAscStackQueueEnqueueRefusesWithoutMergeRight(t *testing.T) {
	f := newQFixture(t)
	// The real check: user 5 has no write access to repository 1.
	ascQueueMayMerge = pull_service.IsUserAllowedToMerge
	_, err := AscQueueEnqueue(t.Context(), qUser(t, 5), f.top)
	require.ErrorIs(t, err, ErrAscQueueNotAllowed)
	assert.Empty(t, qStates(t, f), "nothing is queued for a user who may not merge")

	// Not stored in asc: no queue.
	ascQueueBacked = func(*repo_model.Repository) (bool, error) { return false, nil }
	_, err = AscQueueEnqueue(t.Context(), qUser(t, 2), f.top)
	require.ErrorIs(t, err, ErrAscQueueDisabled)

	// A deactivated user never enqueues.
	ascQueueBacked = func(*repo_model.Repository) (bool, error) { return true, nil }
	u := qUser(t, 2)
	u.ProhibitLogin = true
	_, err = AscQueueEnqueue(t.Context(), u, f.top)
	require.ErrorIs(t, err, ErrAscQueueNotAllowed)
}

func TestAscStackQueueLandsLargestReadyPrefix(t *testing.T) {
	f := newQFixture(t)
	_, err := AscQueueEnqueue(t.Context(), qUser(t, 2), f.top)
	require.NoError(t, err)

	// The top is not ready (checks pending): the largest ready prefix is the bottom alone.
	f.lander.notReady[f.top.ID] = true
	n, err := RunAscStackQueue(t.Context(), f.top.BaseRepoID, "master")
	require.NoError(t, err)
	assert.Equal(t, 1, n)
	assert.Equal(t, []int64{f.top.ID, f.bottom.ID}, f.lander.lands, "the largest prefix is tried first")
	assert.Equal(t, map[int64]string{f.bottom.ID: AscQueueMerged, f.top.ID: AscQueueQueued}, qStates(t, f))

	// Ready now: the top lands. One run, one land of the remaining prefix.
	f.lander.notReady = map[int64]bool{}
	f.lander.lands = nil
	n, err = RunAscStackQueue(t.Context(), f.top.BaseRepoID, "master")
	require.NoError(t, err)
	assert.Equal(t, 1, n)
	assert.Equal(t, []int64{f.top.ID}, f.lander.lands)
	assert.Equal(t, map[int64]string{f.bottom.ID: AscQueueMerged, f.top.ID: AscQueueMerged}, qStates(t, f))
	assert.Equal(t, 1, f.lander.maxFlight)

	// Nothing left: a run does nothing.
	f.lander.lands = nil
	n, err = RunAscStackQueue(t.Context(), f.top.BaseRepoID, "master")
	require.NoError(t, err)
	assert.Zero(t, n)
	assert.Empty(t, f.lander.lands)
}

func TestAscStackQueueWholeStackLandsInOneCall(t *testing.T) {
	f := newQFixture(t)
	_, err := AscQueueEnqueue(t.Context(), qUser(t, 2), f.top)
	require.NoError(t, err)
	n, err := RunAscStackQueue(t.Context(), f.top.BaseRepoID, "master")
	require.NoError(t, err)
	assert.Equal(t, 2, n)
	assert.Equal(t, []int64{f.top.ID}, f.lander.lands, "one atomic land of 1..N")
}

func TestAscStackQueuePauseStopsLands(t *testing.T) {
	f := newQFixture(t)
	_, err := AscQueueEnqueue(t.Context(), qUser(t, 2), f.top)
	require.NoError(t, err)
	repo := unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{ID: f.top.BaseRepoID})

	// Only an administrator pauses.
	require.ErrorIs(t, AscQueueSetPaused(t.Context(), qUser(t, 5), repo, "master", true), ErrAscQueueNoPermission)
	paused, err := AscQueuePaused(t.Context(), repo.ID, "master")
	require.NoError(t, err)
	assert.False(t, paused)

	require.NoError(t, AscQueueSetPaused(t.Context(), qUser(t, 1), repo, "master", true))
	n, err := RunAscStackQueue(t.Context(), repo.ID, "master")
	require.NoError(t, err)
	assert.Zero(t, n)
	assert.Empty(t, f.lander.lands, "a paused branch lands nothing")
	_, err = AscQueueLandNow(t.Context(), qUser(t, 2), f.top)
	require.ErrorIs(t, err, ErrAscQueuePaused)

	require.ErrorIs(t, AscQueueSetPaused(t.Context(), qUser(t, 5), repo, "master", false), ErrAscQueueNoPermission)
	require.NoError(t, AscQueueSetPaused(t.Context(), qUser(t, 1), repo, "master", false))
	n, err = RunAscStackQueue(t.Context(), repo.ID, "master")
	require.NoError(t, err)
	assert.Equal(t, 2, n)
}

func TestAscStackQueueRevalidateOnContentMerge(t *testing.T) {
	f := newQFixture(t)
	_, err := AscQueueEnqueue(t.Context(), qUser(t, 2), f.top)
	require.NoError(t, err)
	repoID := f.top.BaseRepoID

	// Default: lands carry no_content_merge; a content merge that cannot be restacked blocks the stack
	// until a head changes.
	f.lander.needsCM = true
	f.lander.restackErr = errors.New("conflict")
	n, err := RunAscStackQueue(t.Context(), repoID, "master")
	require.NoError(t, err)
	assert.Zero(t, n)
	assert.Equal(t, []bool{true}, f.lander.flags)
	assert.Equal(t, 1, f.lander.restacks)
	assert.Equal(t, map[int64]string{f.bottom.ID: AscQueueBlocked, f.top.ID: AscQueueBlocked}, qStates(t, f))

	// Blocked entries are not tried again while the heads are the same.
	f.lander.lands, f.lander.flags = nil, nil
	_, err = RunAscStackQueue(t.Context(), repoID, "master")
	require.NoError(t, err)
	assert.Empty(t, f.lander.lands)

	// The author pushes a new version: back in the queue and landed.
	f.lander.needsCM = false
	f.heads[f.top.ID] = "cccc"
	n, err = RunAscStackQueue(t.Context(), repoID, "master")
	require.NoError(t, err)
	assert.Equal(t, 2, n)
}

func TestAscStackQueueRestackedStackWaitsForChecks(t *testing.T) {
	f := newQFixture(t)
	_, err := AscQueueEnqueue(t.Context(), qUser(t, 2), f.top)
	require.NoError(t, err)
	// The restack works: the entries go back to queued, where the checks of the new versions gate the next
	// land. The fake keeps answering "needs a content merge", so the run ends at its cap without a landing.
	f.lander.needsCM = true
	n, err := RunAscStackQueue(t.Context(), f.top.BaseRepoID, "master")
	require.NoError(t, err)
	assert.Zero(t, n)
	assert.Positive(t, f.lander.restacks)
	assert.Equal(t, map[int64]string{f.bottom.ID: AscQueueQueued, f.top.ID: AscQueueQueued}, qStates(t, f))
}

func TestAscStackQueueRevalidateSetting(t *testing.T) {
	f := newQFixture(t)
	for in, want := range map[string]bool{"": true, "on_content_merge": true, "garbage": true, "ON_CONTENT_MERGE": true, "off": false, " OFF ": false} {
		ascQueueRevalidateSetting = func() string { return in }
		assert.Equal(t, want, ascQueueNoContentMerge(), "setting %q", in)
	}

	// off omits the flag on the land.
	ascQueueRevalidateSetting = func() string { return "off" }
	_, err := AscQueueEnqueue(t.Context(), qUser(t, 2), f.top)
	require.NoError(t, err)
	_, err = RunAscStackQueue(t.Context(), f.top.BaseRepoID, "master")
	require.NoError(t, err)
	assert.Equal(t, []bool{false}, f.lander.flags)
}

func TestAscStackQueueDefaultLanderFailsClosedWithoutFlagSupport(t *testing.T) {
	_, err := ascPullLander{}.Land(t.Context(), nil, access_model.Permission{}, nil, true)
	require.ErrorIs(t, err, ErrAscQueueUnsupported)
}

func TestAscStackQueueOneLandInFlightPerBranch(t *testing.T) {
	f := newQFixture(t)
	_, err := AscQueueEnqueue(t.Context(), qUser(t, 2), f.top)
	require.NoError(t, err)
	repoID := f.top.BaseRepoID

	unlock, ok, err := ascQueueLock(t.Context(), repoID, "master")
	require.NoError(t, err)
	require.True(t, ok)
	_, ok2, err := ascQueueLock(t.Context(), repoID, "master")
	require.NoError(t, err)
	assert.False(t, ok2, "a second worker does not get the branch")
	n, err := RunAscStackQueue(t.Context(), repoID, "master")
	require.NoError(t, err)
	assert.Zero(t, n)
	assert.Empty(t, f.lander.lands, "a worker that does not hold the lease lands nothing")
	_, err = AscQueueLandNow(t.Context(), qUser(t, 2), f.top)
	require.ErrorIs(t, err, ErrAscQueueBusy)

	// A lease that ran out (a crashed worker) can be taken over.
	prev := ascQueueNow
	ascQueueNow = func() time.Time { return prev().Add(2 * ascQueueLease) }
	t.Cleanup(func() { ascQueueNow = prev })
	unlock2, ok3, err := ascQueueLock(t.Context(), repoID, "master")
	require.NoError(t, err)
	assert.True(t, ok3)
	unlock2()
	unlock()

	// Another branch has its own lease.
	u3, ok4, err := ascQueueLock(t.Context(), repoID, "other")
	require.NoError(t, err)
	assert.True(t, ok4)
	u3()
}

func TestAscStackQueueCancel(t *testing.T) {
	f := newQFixture(t)
	_, err := AscQueueEnqueue(t.Context(), qUser(t, 2), f.top)
	require.NoError(t, err)

	// Another ordinary user may not cancel the entry of user 2.
	require.ErrorIs(t, AscQueueCancel(t.Context(), qUser(t, 5), f.bottom), ErrAscQueueNotAllowed)
	assert.Equal(t, map[int64]string{f.bottom.ID: AscQueueQueued, f.top.ID: AscQueueQueued}, qStates(t, f))

	// Cancelling the bottom cancels the top too: the top would land the bottom.
	require.NoError(t, AscQueueCancel(t.Context(), qUser(t, 2), f.bottom))
	assert.Equal(t, map[int64]string{f.bottom.ID: AscQueueCancelled, f.top.ID: AscQueueCancelled}, qStates(t, f))
	n, err := RunAscStackQueue(t.Context(), f.top.BaseRepoID, "master")
	require.NoError(t, err)
	assert.Zero(t, n)
	assert.Empty(t, f.lander.lands)

	// A site admin may cancel what another user queued.
	_, err = AscQueueEnqueue(t.Context(), qUser(t, 2), f.top)
	require.NoError(t, err)
	require.NoError(t, AscQueueCancel(t.Context(), qUser(t, 1), f.top))
	assert.Equal(t, AscQueueCancelled, qStates(t, f)[f.top.ID])
}

func TestAscStackQueueUnavailableDoerCancels(t *testing.T) {
	f := newQFixture(t)
	_, err := AscQueueEnqueue(t.Context(), qUser(t, 2), f.top)
	require.NoError(t, err)

	// The user who queued it is deactivated afterwards: nothing lands on the old grant.
	_, err = db.GetEngine(t.Context()).ID(2).Cols("prohibit_login").Update(&user_model.User{ProhibitLogin: true})
	require.NoError(t, err)
	for range 3 {
		_, err = RunAscStackQueue(t.Context(), f.top.BaseRepoID, "master")
		require.NoError(t, err)
	}
	assert.Empty(t, f.lander.lands)
	for _, st := range qStates(t, f) {
		assert.Equal(t, AscQueueCancelled, st)
	}
}

func TestAscStackQueueRecoversLandingEntries(t *testing.T) {
	f := newQFixture(t)
	_, err := AscQueueEnqueue(t.Context(), qUser(t, 2), f.top)
	require.NoError(t, err)
	require.NoError(t, ascQueueSetState(t.Context(), AscQueueLanding, "", "", []int64{f.bottom.ID, f.top.ID}))
	// The bottom had landed before the crash.
	_, err = db.GetEngine(t.Context()).ID(f.bottom.ID).Cols("has_merged").Update(&issues_model.PullRequest{HasMerged: true})
	require.NoError(t, err)

	f.lander.notReady[f.top.ID] = true
	_, err = RunAscStackQueue(t.Context(), f.top.BaseRepoID, "master")
	require.NoError(t, err)
	assert.Equal(t, map[int64]string{f.bottom.ID: AscQueueMerged, f.top.ID: AscQueueQueued}, qStates(t, f))
}

// changeID gives each (stack, position) its own 32 letters k-z.
func changeID(stack, pos int) string {
	b := []byte("kkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkk")
	b[0] = 'k' + byte(pos)
	b[1] = 'k' + byte(stack/16)
	b[2] = 'k' + byte(stack%16)
	return string(b)
}

// Twenty independent stacks of two, queued bottoms first and then tops, land once each and never in parallel:
// no change is lost and none is landed twice.
func TestAscStackQueueManyStacksLandOnceEach(t *testing.T) {
	f := newQFixture(t)
	ctx := t.Context()
	e := db.GetEngine(ctx)
	const stacks = 20
	var bottoms, tops []*issues_model.PullRequest
	for i := range stacks {
		var bottomID int64
		for j := range 2 {
			pr := &issues_model.PullRequest{
				Type: issues_model.PullRequestGitea, Index: int64(1000 + i*2 + j), BaseRepoID: f.top.BaseRepoID,
				HeadRepoID: f.top.BaseRepoID, BaseBranch: "master", HeadBranch: fmt.Sprintf("s%d-%d", i, j),
			}
			_, err := e.Insert(pr)
			require.NoError(t, err)
			require.NoError(t, issues_model.SetPullRequestAscStack(ctx, pr, changeID(i, j), bottomID))
			if j == 0 {
				bottomID = pr.ID
				bottoms = append(bottoms, pr)
			} else {
				tops = append(tops, pr)
			}
		}
	}
	for _, p := range append(append([]*issues_model.PullRequest{}, bottoms...), tops...) {
		row, err := issues_model.GetPullRequestAscStack(ctx, p.ID)
		require.NoError(t, err)
		_, err = e.Insert(&AscStackQueueEntry{RepoID: p.BaseRepoID, Branch: "master", PullID: p.ID, AscChange: row.AscChange, DoerID: 2, State: AscQueueQueued})
		require.NoError(t, err)
	}

	n, err := RunAscStackQueue(ctx, f.top.BaseRepoID, "master")
	require.NoError(t, err)
	assert.Equal(t, stacks*2, n)
	assert.Equal(t, 1, f.lander.maxFlight)

	seen := map[int64]int{}
	for _, id := range f.lander.lands {
		seen[id]++
	}
	assert.Len(t, f.lander.lands, stacks, "one atomic land per stack")
	for id, c := range seen {
		assert.Equal(t, 1, c, "pull %d landed twice", id)
	}
	for _, p := range append(append([]*issues_model.PullRequest{}, bottoms...), tops...) {
		assert.True(t, unittest.AssertExistsAndLoadBean(t, &issues_model.PullRequest{ID: p.ID}).HasMerged, "pull %d lost", p.ID)
	}
}
