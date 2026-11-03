// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package webhook

import (
	"testing"

	"forgejo.org/models/db"
	"forgejo.org/models/unittest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A streak counts the failures since the last success, newest first, and stops at the limit.
func TestFailingStreaks(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())

	streaks, err := FailingStreaks(db.DefaultContext, []int64{1, 2, 3}, 10)
	require.NoError(t, err)
	assert.NotContains(t, streaks, int64(1), "the latest delivery of hook 1 succeeded")
	assert.NotContains(t, streaks, int64(2), "hook 2 never delivered")
	assert.Equal(t, 1, streaks[3])

	deliver := func(succeed bool) {
		task, err := CreateHookTask(db.DefaultContext, &HookTask{HookID: 1, PayloadVersion: 2})
		require.NoError(t, err)
		task.IsDelivered, task.IsSucceed = true, succeed
		require.NoError(t, UpdateHookTask(db.DefaultContext, task))
	}
	for range 4 {
		deliver(false)
	}
	_, err = CreateHookTask(db.DefaultContext, &HookTask{HookID: 1, PayloadVersion: 2})
	require.NoError(t, err, "a task still queued does not count")

	streaks, err = FailingStreaks(db.DefaultContext, []int64{1}, 10)
	require.NoError(t, err)
	assert.Equal(t, 4, streaks[1])

	streaks, err = FailingStreaks(db.DefaultContext, []int64{1}, 3)
	require.NoError(t, err)
	assert.Equal(t, 3, streaks[1], "the streak stops at the limit")

	deliver(true)
	streaks, err = FailingStreaks(db.DefaultContext, []int64{1}, 10)
	require.NoError(t, err)
	assert.NotContains(t, streaks, int64(1))

	empty, err := FailingStreaks(db.DefaultContext, nil, 10)
	require.NoError(t, err)
	assert.Empty(t, empty)
}

// Pausing a hook changes only whether it delivers.
func TestSetWebhookActive(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())

	w := unittest.AssertExistsAndLoadBean(t, &Webhook{ID: 1})
	require.False(t, w.IsActive, "the fixture hook starts paused")
	require.NoError(t, SetWebhookActive(db.DefaultContext, w, true))
	after := unittest.AssertExistsAndLoadBean(t, &Webhook{ID: 1})
	assert.True(t, after.IsActive)
	assert.Equal(t, w.URL, after.URL)
	assert.Equal(t, w.Events, after.Events)

	require.NoError(t, SetWebhookActive(db.DefaultContext, after, false))
	assert.False(t, unittest.AssertExistsAndLoadBean(t, &Webhook{ID: 1}).IsActive)
}
