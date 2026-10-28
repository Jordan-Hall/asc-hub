// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package user_test

import (
	"testing"

	"forgejo.org/models/db"
	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/timeutil"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAscNotifyLog(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	ctx := t.Context()

	last, err := user_model.LastAscNotified(ctx, 2, []int64{1, 2})
	require.NoError(t, err)
	assert.Empty(t, last)

	require.NoError(t, user_model.RecordAscNotification(ctx, 2, 1, "checks_failed", "title", "/p", "", true))
	require.NoError(t, user_model.RecordAscNotification(ctx, 2, 1, "merge_queue", "title", "/p", "", false))
	require.NoError(t, user_model.RecordAscNotification(ctx, 4, 2, "agent_question", "title", "/p", "", false))
	last, err = user_model.LastAscNotified(ctx, 2, []int64{1, 2})
	require.NoError(t, err)
	assert.NotZero(t, last[1])
	assert.Zero(t, last[2], "another person's notification is not mine")

	// entries older than 90 days go
	_, err = db.GetEngine(ctx).Exec("UPDATE `asc_notify_log` SET sent_unix = ? WHERE user_id = 4", timeutil.TimeStampNow().Add(-100*86400))
	require.NoError(t, err)
	n, err := user_model.DeleteOldAscNotifyLog(ctx)
	require.NoError(t, err)
	assert.EqualValues(t, 1, n)
}

func TestAscNotificationEventIDIsToldOnce(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	ctx := t.Context()
	has, err := user_model.HasAscNotificationEvent(ctx, 2, "ev-1")
	require.NoError(t, err)
	assert.False(t, has)
	require.NoError(t, user_model.RecordAscNotification(ctx, 2, 1, "merge_queue", "t", "/p", "ev-1", false))
	has, _ = user_model.HasAscNotificationEvent(ctx, 2, "ev-1")
	assert.True(t, has)
	has, _ = user_model.HasAscNotificationEvent(ctx, 4, "ev-1")
	assert.False(t, has, "told to one person, not to the other")
	has, _ = user_model.HasAscNotificationEvent(ctx, 2, "")
	assert.False(t, has, "an event without an id is never a repeat")
}
