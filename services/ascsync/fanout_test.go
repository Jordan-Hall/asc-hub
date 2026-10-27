// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package ascsync

import (
	"context"
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

func TestWaitForRefsStopsWithTheContext(t *testing.T) {
	fx := newFixture(t)
	c := fx.commit("one")
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	err := WaitForRefs(ctx, fx.repo, []PushRef{{Name: "refs/heads/never", Old: zeroID, New: c}})
	require.Error(t, err)
	assert.NotErrorIs(t, err, ErrBridgeBehind)
}

func TestRememberedPusherIsFoundByRefAndCommit(t *testing.T) {
	repo := &repo_model.Repository{ID: 41}
	other := &repo_model.Repository{ID: 42}
	RememberPusher(repo, 7, []PushRef{{Name: "refs/heads/a", New: "aaaa"}, {Name: "refs/heads/b", New: zeroID}})

	id, ok := PusherOf(repo, "refs/heads/a", "aaaa")
	assert.True(t, ok)
	assert.Equal(t, int64(7), id)
	id, ok = PusherOf(repo, "refs/heads/b", zeroID)
	assert.True(t, ok, "a deletion is remembered under the zero id")
	assert.Equal(t, int64(7), id)

	_, ok = PusherOf(repo, "refs/heads/a", "bbbb")
	assert.False(t, ok, "another commit of the same ref is another push")
	_, ok = PusherOf(other, "refs/heads/a", "aaaa")
	assert.False(t, ok, "another repository is another push")
}

func TestRememberedPusherNeedsAUserAndExpires(t *testing.T) {
	repo := &repo_model.Repository{ID: 43}
	RememberPusher(repo, 0, []PushRef{{Name: "refs/heads/a", New: "cccc"}})
	RememberPusher(repo, -2, []PushRef{{Name: "refs/heads/a", New: "cccc"}})
	_, ok := PusherOf(repo, "refs/heads/a", "cccc")
	assert.False(t, ok, "nobody to attribute")

	now := time.Now()
	oldClock := pusherClock
	pusherClock = func() time.Time { return now }
	t.Cleanup(func() { pusherClock = oldClock })

	RememberPusher(repo, 9, []PushRef{{Name: "refs/heads/a", New: "dddd"}})
	now = now.Add(pusherTTL - time.Second)
	_, ok = PusherOf(repo, "refs/heads/a", "dddd")
	assert.True(t, ok)
	now = now.Add(2 * time.Second)
	_, ok = PusherOf(repo, "refs/heads/a", "dddd")
	assert.False(t, ok, "an old push is forgotten")
}
