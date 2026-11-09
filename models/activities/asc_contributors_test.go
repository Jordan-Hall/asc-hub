// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package activities_test

import (
	"testing"
	"time"

	activities_model "forgejo.org/models/activities"
	"forgejo.org/models/db"
	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/timeutil"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTheContributorsOfTheRepositoryLists(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	ctx := t.Context()
	e := db.GetEngine(ctx)
	now := time.Now()
	since := timeutil.TimeStamp(now.AddDate(0, 0, -activities_model.AscContributorDays).Unix())

	_, err := e.Exec("DELETE FROM `action`")
	require.NoError(t, err)
	act := func(t *testing.T, receiver, actor, repo int64, at time.Time) {
		t.Helper()
		_, err := e.Exec("INSERT INTO `action` (user_id, op_type, act_user_id, repo_id, is_private, created_unix) VALUES (?, 5, ?, ?, ?, ?)",
			receiver, actor, repo, false, at.Unix())
		require.NoError(t, err)
	}
	_, err = e.Exec("UPDATE `user` SET type = ? WHERE id = 30", user_model.UserTypeBot)
	require.NoError(t, err)
	hourAgo := now.Add(-time.Hour)
	// repo 1: user2 acted three times (each action also copied to the feed of the watcher user4), the agent user30
	// twice, user4 and user5 once each, and a deleted user once
	for range 3 {
		act(t, 2, 2, 1, hourAgo)
		act(t, 4, 2, 1, hourAgo)
		act(t, 4, 2, 1, hourAgo)
	}
	act(t, 30, 30, 1, hourAgo)
	act(t, 30, 30, 1, hourAgo)
	act(t, 5, 5, 1, hourAgo)
	act(t, 4, 4, 1, hourAgo)
	act(t, 9999, 9999, 1, hourAgo)
	// repo 2: only an action from before the window
	act(t, 2, 2, 2, now.AddDate(0, 0, -activities_model.AscContributorDays-1))

	got, err := activities_model.AscTopContributors(ctx, []int64{1, 2, 3}, since)
	require.NoError(t, err)
	ids := func(us []*user_model.User) []int64 {
		out := make([]int64, 0, len(us))
		for _, u := range us {
			out = append(out, u.ID)
		}
		return out
	}

	t.Run("The most active come first, a tie goes to the lower id, and each action counts once", func(t *testing.T) {
		assert.Equal(t, []int64{2, 30, 4, 5}, ids(got[1]))
	})
	t.Run("An agent is in the stack as its bot account", func(t *testing.T) {
		require.Len(t, got[1], 4)
		assert.True(t, got[1][1].IsBot())
	})
	t.Run("A repository without actions in the window has no stack", func(t *testing.T) {
		assert.NotContains(t, got, int64(2))
		assert.NotContains(t, got, int64(3))
	})
	t.Run("No repositories, no query", func(t *testing.T) {
		none, err := activities_model.AscTopContributors(ctx, nil, since)
		require.NoError(t, err)
		assert.Empty(t, none)
	})
}
