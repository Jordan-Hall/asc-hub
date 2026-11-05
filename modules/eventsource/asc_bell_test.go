// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package eventsource

import (
	"testing"

	activities_model "forgejo.org/models/activities"
	"forgejo.org/models/db"
	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/test"
	"forgejo.org/modules/timeutil"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAscBellCounts(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	ctx := db.DefaultContext
	require.NoError(t, user_model.RecordAscEvent(ctx, &user_model.AscNotifyLog{UserID: 2, RepoID: 1, Event: "checks_failed", Title: "x"}))
	require.NoError(t, user_model.RecordAscEvent(ctx, &user_model.AscNotifyLog{UserID: 4, RepoID: 1, Event: "merge_queue", Title: "y"}))
	now := timeutil.TimeStampNow()
	hub := []activities_model.UserIDCount{{UserID: 2, Count: 2}}

	func() {
		defer test.MockVariableValue(&setting.Asc.Enabled, false)()
		assert.Equal(t, hub, ascBellCounts(ctx, hub, now-60, now+1), "without ASC the counts of the hub stay")
	}()

	defer test.MockVariableValue(&setting.Asc.Enabled, true)()
	got := map[int64]int64{}
	for _, c := range ascBellCounts(ctx, hub, now-60, now+1) {
		got[c.UserID] = c.Count
	}
	assert.EqualValues(t, 3, got[2], "the hub's two and one event")
	assert.EqualValues(t, 1, got[4], "told about an event with nothing new from the hub")

	got = map[int64]int64{}
	for _, c := range ascBellCounts(ctx, nil, now+1, now+60) {
		got[c.UserID] = c.Count
	}
	assert.Empty(t, got, "nobody told in the window")
}
