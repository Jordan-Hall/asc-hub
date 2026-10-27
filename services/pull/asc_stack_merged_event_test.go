// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package pull

import (
	"context"
	"sync"
	"testing"

	issues_model "forgejo.org/models/issues"
	user_model "forgejo.org/models/user"
	notify_service "forgejo.org/services/notify"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mergedRecorder records the merged events of pull requests.
type mergedRecorder struct {
	notify_service.NullNotifier
	mu    sync.Mutex
	plain []int64
	auto  []int64
	on    bool
}

func (r *mergedRecorder) MergePullRequest(_ context.Context, _ *user_model.User, pr *issues_model.PullRequest) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.on {
		r.plain = append(r.plain, pr.ID)
	}
}

func (r *mergedRecorder) AutoMergePullRequest(_ context.Context, _ *user_model.User, pr *issues_model.PullRequest) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.on {
		r.auto = append(r.auto, pr.ID)
	}
}

var (
	mergedRec     = &mergedRecorder{}
	mergedRecOnce sync.Once
)

func recordMerged(t *testing.T) *mergedRecorder {
	t.Helper()
	mergedRecOnce.Do(func() { notify_service.RegisterNotifier(mergedRec) })
	mergedRec.mu.Lock()
	mergedRec.plain, mergedRec.auto, mergedRec.on = nil, nil, true
	mergedRec.mu.Unlock()
	t.Cleanup(func() {
		mergedRec.mu.Lock()
		mergedRec.on = false
		mergedRec.mu.Unlock()
	})
	return mergedRec
}

// A landed stack raises the merged event of every pull request that landed, once each, and none for a pull
// request that stayed open. The event is the automatic one when the queue landed it.
func TestAscStackMergeRaisesMergedEvent(t *testing.T) {
	t.Run("an ordinary merge", func(t *testing.T) {
		rec := recordMerged(t)
		_, top, _ := ascMergeFixture(t)
		doer, perm := ascMergePerm(t, top, 2)
		_, err := MergeAscStack(t.Context(), doer, perm, top)
		require.NoError(t, err)
		assert.ElementsMatch(t, []int64{2, 5}, rec.plain)
		assert.Empty(t, rec.auto)
	})

	t.Run("a land by the queue", func(t *testing.T) {
		rec := recordMerged(t)
		_, top, _ := ascMergeFixture(t)
		doer, perm := ascMergePerm(t, top, 2)
		_, err := MergeAscStackWith(t.Context(), doer, perm, top, AscMergeOptions{Auto: true})
		require.NoError(t, err)
		assert.ElementsMatch(t, []int64{2, 5}, rec.auto)
		assert.Empty(t, rec.plain)
	})

	t.Run("only the prefix that landed", func(t *testing.T) {
		rec := recordMerged(t)
		bottom, _, _ := ascMergeFixture(t)
		doer, perm := ascMergePerm(t, bottom, 2)
		_, err := MergeAscStack(t.Context(), doer, perm, bottom)
		require.NoError(t, err)
		assert.Equal(t, []int64{2}, rec.plain)
	})

	t.Run("a refused land raises nothing", func(t *testing.T) {
		rec := recordMerged(t)
		_, top, _ := ascMergeFixture(t)
		doer, perm := ascMergePerm(t, top, 5)
		_, err := MergeAscStack(t.Context(), doer, perm, top)
		require.ErrorIs(t, err, ErrAscStackNotAllowed)
		assert.Empty(t, rec.plain)
		assert.Empty(t, rec.auto)
	})
}
