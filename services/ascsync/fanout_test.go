// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package ascsync

import (
	"context"
	"fmt"
	"testing"
	"time"

	repo_model "forgejo.org/models/repo"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWaitForRefsWaitsUntilTheRepositoryHoldsTheCommit(t *testing.T) {
	fx := newFixture(t)
	c := fx.commit("one")
	go func() {
		time.Sleep(120 * time.Millisecond)
		gitRun(t, fx.bridge, nil, "", "update-ref", "refs/heads/feature", c)
	}()

	start := time.Now()
	require.NoError(t, WaitForRefs(t.Context(), fx.repo, []PushRef{{Name: "refs/heads/feature", Old: zeroID, New: c}}))
	assert.GreaterOrEqual(t, time.Since(start), 100*time.Millisecond, "the branch was not there yet")
	assert.Equal(t, c, fx.bridgeRef("refs/heads/feature"))
}

func TestWaitForRefsWaitsForADeletion(t *testing.T) {
	fx := newFixture(t)
	c := fx.commit("one")
	gitRun(t, fx.bridge, nil, "", "update-ref", "refs/heads/gone", c)
	go func() {
		time.Sleep(120 * time.Millisecond)
		gitRun(t, fx.bridge, nil, "", "update-ref", "-d", "refs/heads/gone")
	}()

	start := time.Now()
	require.NoError(t, WaitForRefs(t.Context(), fx.repo, []PushRef{{Name: "refs/heads/gone", Old: c, New: zeroID}}))
	assert.GreaterOrEqual(t, time.Since(start), 100*time.Millisecond)
	assert.Empty(t, fx.bridgeRef("refs/heads/gone"))
}

func TestWaitForRefsGivesUpWhenTheRepositoryNeverFollows(t *testing.T) {
	fx := newFixture(t)
	c := fx.commit("one")
	old := fanOutWait
	fanOutWait = 150 * time.Millisecond
	t.Cleanup(func() { fanOutWait = old })

	err := WaitForRefs(t.Context(), fx.repo, []PushRef{{Name: "refs/heads/never", Old: zeroID, New: c}})
	require.ErrorIs(t, err, ErrBridgeBehind)
}

func TestWaitForRefsDoesNotWaitForARefTheHubTurnsIntoAPullRequest(t *testing.T) {
	fx := newFixture(t)
	c := fx.commit("one")
	gitRun(t, fx.bridge, nil, "", "update-ref", "refs/heads/feature", c)
	old := fanOutWait
	fanOutWait = 5 * time.Second
	t.Cleanup(func() { fanOutWait = old })

	start := time.Now()
	require.NoError(t, WaitForRefs(t.Context(), fx.repo, []PushRef{
		{Name: "refs/for/main/asc-topic", Old: zeroID, New: c},
		{Name: "refs/heads/feature", Old: zeroID, New: c},
	}))
	assert.Less(t, time.Since(start), time.Second, "refs/for/... is never stored, so nothing is waited for")
}

func TestWaitForRefsStopsWithTheContext(t *testing.T) {
	fx := newFixture(t)
	c := fx.commit("one")
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	err := WaitForRefs(ctx, fx.repo, []PushRef{{Name: "refs/heads/never", Old: zeroID, New: c}})
	require.Error(t, err)
	assert.NotErrorIs(t, err, ErrBridgeBehind)
}

// resetPushers empties the memory and restores the clock after a test.
func resetPushers(t *testing.T) {
	t.Helper()
	pusherMu.Lock()
	clear(pushers)
	pusherMu.Unlock()
	oldClock := pusherClock
	t.Cleanup(func() {
		pusherMu.Lock()
		clear(pushers)
		pusherMu.Unlock()
		pusherClock = oldClock
	})
}

func pusherCount(repo *repo_model.Repository) int {
	pusherMu.Lock()
	defer pusherMu.Unlock()
	if rp := pushers[repo.ID]; rp != nil {
		return len(rp.list)
	}
	return 0
}

func ref(name, old, new string) PushRef { return PushRef{Name: name, Old: old, New: new} }

func TestClaimedPusherIsUsedOnceAndOnlyAsAWhole(t *testing.T) {
	resetPushers(t)
	repo := &repo_model.Repository{ID: 41}
	other := &repo_model.Repository{ID: 42}
	a, b := ref("refs/heads/a", zeroID, "aaaa"), ref("refs/heads/b", "bbbb", zeroID)
	_, err := RememberPusher(repo, 7, []PushRef{a, b})
	require.NoError(t, err)

	_, ok := ClaimPusher(other, []PushRef{a})
	assert.False(t, ok, "another repository is another push")
	_, ok = ClaimPusher(repo, []PushRef{ref("refs/heads/a", zeroID, "cccc")})
	assert.False(t, ok, "another commit of the same ref is another push")
	_, ok = ClaimPusher(repo, []PushRef{a, ref("refs/heads/unknown", zeroID, "dddd")})
	assert.False(t, ok, "one ref change that nobody remembers makes the whole call unattributed")

	id, ok := ClaimPusher(repo, []PushRef{a})
	assert.True(t, ok)
	assert.Equal(t, int64(7), id)
	_, ok = ClaimPusher(repo, []PushRef{a})
	assert.False(t, ok, "a ref change is attributed once")

	id, ok = ClaimPusher(repo, []PushRef{b})
	assert.True(t, ok, "a deletion is remembered with the commit it deletes")
	assert.Equal(t, int64(7), id)
	assert.Zero(t, pusherCount(repo), "a push with nothing left is gone")
	_, ok = ClaimPusher(repo, nil)
	assert.False(t, ok)
}

func TestACallOfTwoPushesIsAttributedToNobody(t *testing.T) {
	resetPushers(t)
	repo := &repo_model.Repository{ID: 44}
	a, b := ref("refs/heads/a", zeroID, "aaaa"), ref("refs/heads/b", zeroID, "bbbb")
	_, _ = RememberPusher(repo, 7, []PushRef{a})
	_, _ = RememberPusher(repo, 8, []PushRef{b})

	_, ok := ClaimPusher(repo, []PushRef{a, b})
	assert.False(t, ok, "the ref changes belong to different pushes")
	assert.Equal(t, 2, pusherCount(repo), "nothing was used up")
	id, ok := ClaimPusher(repo, []PushRef{b})
	assert.True(t, ok)
	assert.Equal(t, int64(8), id)
	id, ok = ClaimPusher(repo, []PushRef{a})
	assert.True(t, ok)
	assert.Equal(t, int64(7), id)
}

func TestAnAmbiguousCallIsAttributedToNobody(t *testing.T) {
	resetPushers(t)
	repo := &repo_model.Repository{ID: 45}
	a := ref("refs/heads/a", zeroID, "aaaa")
	_, _ = RememberPusher(repo, 7, []PushRef{a})
	_, _ = RememberPusher(repo, 8, []PushRef{a})
	_, ok := ClaimPusher(repo, []PushRef{a})
	assert.False(t, ok, "two users fit one ref change")

	// Deleting the same ref from different commits is not the same ref change.
	del7, del8 := ref("refs/heads/x", "1111", zeroID), ref("refs/heads/x", "2222", zeroID)
	_, _ = RememberPusher(repo, 7, []PushRef{del7})
	_, _ = RememberPusher(repo, 8, []PushRef{del8})
	id, ok := ClaimPusher(repo, []PushRef{del8})
	assert.True(t, ok, "a deletion is never claimed through a key another user shares")
	assert.Equal(t, int64(8), id)

	// The same user twice is not ambiguous.
	same := ref("refs/heads/s", zeroID, "ssss")
	_, _ = RememberPusher(repo, 9, []PushRef{same})
	_, _ = RememberPusher(repo, 9, []PushRef{same})
	id, ok = ClaimPusher(repo, []PushRef{same})
	assert.True(t, ok)
	assert.Equal(t, int64(9), id)
}

func TestAPushThatDidNotHappenIsForgotten(t *testing.T) {
	resetPushers(t)
	repo := &repo_model.Repository{ID: 46}
	a := ref("refs/heads/a", zeroID, "aaaa")
	tok, err := RememberPusher(repo, 7, []PushRef{a})
	require.NoError(t, err)
	tok.Forget()
	tok.Forget() // twice is harmless
	PushToken{}.Forget()
	_, ok := ClaimPusher(repo, []PushRef{a})
	assert.False(t, ok, "a forgotten push cannot be claimed")
	assert.Zero(t, pusherCount(repo))

	// Forgetting one push leaves another one of the same repository alone.
	keep, _ := RememberPusher(repo, 8, []PushRef{ref("refs/heads/k", zeroID, "kkkk")})
	drop, _ := RememberPusher(repo, 9, []PushRef{ref("refs/heads/d", zeroID, "dddd")})
	drop.Forget()
	_, ok = ClaimPusher(repo, []PushRef{ref("refs/heads/k", zeroID, "kkkk")})
	assert.True(t, ok)
	_ = keep
}

func TestRememberedPusherNeedsAUserAndExpires(t *testing.T) {
	resetPushers(t)
	repo := &repo_model.Repository{ID: 43}
	a := ref("refs/heads/a", zeroID, "cccc")
	for _, u := range []int64{0, -2} {
		tok, err := RememberPusher(repo, u, []PushRef{a})
		require.NoError(t, err)
		assert.Equal(t, PushToken{}, tok)
	}
	_, ok := ClaimPusher(repo, []PushRef{a})
	assert.False(t, ok, "nobody to attribute")

	now := time.Now()
	pusherClock = func() time.Time { return now }
	_, _ = RememberPusher(repo, 9, []PushRef{a})
	now = now.Add(pusherTTL - time.Second)
	id, ok := ClaimPusher(repo, []PushRef{a})
	assert.True(t, ok)
	assert.Equal(t, int64(9), id)

	_, _ = RememberPusher(repo, 9, []PushRef{a})
	now = now.Add(2 * time.Second)
	_, ok = ClaimPusher(repo, []PushRef{a})
	assert.True(t, ok, "still inside its time")
	_, _ = RememberPusher(repo, 9, []PushRef{a})
	now = now.Add(pusherTTL + time.Second)
	_, ok = ClaimPusher(repo, []PushRef{a})
	assert.False(t, ok, "an old push is forgotten")
	assert.Zero(t, pusherCount(repo))
}

func TestAPushWithTooManyRefsIsRefusedNotTruncated(t *testing.T) {
	resetPushers(t)
	repo := &repo_model.Repository{ID: 47}
	other := &repo_model.Repository{ID: 48}
	keepRef := ref("refs/heads/keep", zeroID, "kkkk")
	_, _ = RememberPusher(other, 5, []PushRef{keepRef})

	var refs []PushRef
	for i := range MaxRefsPerPush + 1 {
		refs = append(refs, ref(fmt.Sprintf("refs/heads/r%d", i), zeroID, "aaaa"))
	}
	tok, err := RememberPusher(repo, 7, refs)
	require.ErrorIs(t, err, ErrTooManyRefs)
	assert.Equal(t, PushToken{}, tok)
	_, ok := ClaimPusher(repo, refs[:1])
	assert.False(t, ok, "not even a part of it is remembered")
	_, ok = ClaimPusher(repo, refs)
	assert.False(t, ok)
	id, ok := ClaimPusher(other, []PushRef{keepRef})
	assert.True(t, ok, "the memory of another repository is untouched")
	assert.Equal(t, int64(5), id)

	// The largest push that is remembered is claimed whole.
	_, err = RememberPusher(repo, 7, refs[:MaxRefsPerPush])
	require.NoError(t, err)
	id, ok = ClaimPusher(repo, refs[:MaxRefsPerPush])
	assert.True(t, ok)
	assert.Equal(t, int64(7), id)
}

// A repository that pushes a lot loses its own oldest pushes and nobody else's.
func TestOneRepositoryCannotEvictAnother(t *testing.T) {
	resetPushers(t)
	flood := &repo_model.Repository{ID: 50}
	quiet := &repo_model.Repository{ID: 51}
	quietRef := ref("refs/heads/quiet", zeroID, "qqqq")
	_, err := RememberPusher(quiet, 6, []PushRef{quietRef})
	require.NoError(t, err)

	for i := range 5 * MaxPushesPerRepo {
		_, err := RememberPusher(flood, 7, []PushRef{ref(fmt.Sprintf("refs/heads/f%d", i), zeroID, fmt.Sprintf("%04d", i))})
		require.NoError(t, err)
	}
	assert.Equal(t, MaxPushesPerRepo, pusherCount(flood), "the repository is held to its cap")
	id, ok := ClaimPusher(quiet, []PushRef{quietRef})
	assert.True(t, ok, "the other repository kept its entry")
	assert.Equal(t, int64(6), id)

	_, ok = ClaimPusher(flood, []PushRef{ref("refs/heads/f0", zeroID, "0000")})
	assert.False(t, ok, "the oldest push of the flooding repository went first")
	last := 5*MaxPushesPerRepo - 1
	id, ok = ClaimPusher(flood, []PushRef{ref(fmt.Sprintf("refs/heads/f%d", last), zeroID, fmt.Sprintf("%04d", last))})
	assert.True(t, ok, "the newest pushes stay")
	assert.Equal(t, int64(7), id)
	first := 5*MaxPushesPerRepo - MaxPushesPerRepo
	_, ok = ClaimPusher(flood, []PushRef{ref(fmt.Sprintf("refs/heads/f%d", first), zeroID, fmt.Sprintf("%04d", first))})
	assert.True(t, ok, "the cap keeps exactly the newest MaxPushesPerRepo")
}

// The number of repositories is bounded too; then the repository that pushed longest ago gives way, not a busy one.
func TestTheNumberOfRepositoriesIsBounded(t *testing.T) {
	resetPushers(t)
	now := time.Now()
	pusherClock = func() time.Time { now = now.Add(time.Millisecond); return now }
	a := ref("refs/heads/a", zeroID, "aaaa")
	for id := range int64(maxPusherRepos) {
		_, _ = RememberPusher(&repo_model.Repository{ID: 1000 + id}, 3, []PushRef{a})
	}
	// The first repository pushes again, so it is no longer the quietest.
	_, _ = RememberPusher(&repo_model.Repository{ID: 1000}, 3, []PushRef{ref("refs/heads/again", zeroID, "bbbb")})
	_, _ = RememberPusher(&repo_model.Repository{ID: 99999}, 4, []PushRef{a})

	pusherMu.Lock()
	n := len(pushers)
	_, firstKept := pushers[1000]
	_, quietKept := pushers[1001]
	pusherMu.Unlock()
	assert.Equal(t, maxPusherRepos, n)
	assert.True(t, firstKept, "a repository that pushed lately stays")
	assert.False(t, quietKept, "the one that pushed longest ago gave way")
	id, ok := ClaimPusher(&repo_model.Repository{ID: 99999}, []PushRef{a})
	assert.True(t, ok)
	assert.Equal(t, int64(4), id)
}
