// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package user_test

import (
	"testing"
	"time"

	"forgejo.org/models/db"
	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTheFiltersOfThePeopleDirectory(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	ctx := t.Context()
	e := db.GetEngine(ctx)
	_, err := e.Exec("UPDATE `user` SET type = ? WHERE id = 30", user_model.UserTypeBot)
	require.NoError(t, err)

	ids := func(t *testing.T, opts *user_model.SearchUserOptions) []int64 {
		t.Helper()
		if opts.OrderBy == "" {
			opts.OrderBy = db.SearchOrderByID
		}
		users, _, err := user_model.SearchUsers(ctx, opts)
		require.NoError(t, err)
		out := make([]int64, 0, len(users))
		for _, u := range users {
			out = append(out, u.ID)
		}
		return out
	}

	t.Run("People and agents finds the bot accounts too", func(t *testing.T) {
		assert.Empty(t, ids(t, &user_model.SearchUserOptions{Type: user_model.UserTypeIndividual, UID: 30}))
		assert.Equal(t, []int64{30}, ids(t, &user_model.SearchUserOptions{Type: user_model.UserTypeIndividual, UID: 30, Asc: user_model.AscUserFilter{WithAgents: true}}))
		assert.Equal(t, []int64{2}, ids(t, &user_model.SearchUserOptions{Type: user_model.UserTypeIndividual, UID: 2, Asc: user_model.AscUserFilter{WithAgents: true}}))
		assert.Equal(t, []int64{30}, ids(t, &user_model.SearchUserOptions{Type: user_model.UserTypeBot}))
	})

	t.Run("Organization keeps the public members, or all of them", func(t *testing.T) {
		assert.Equal(t, []int64{2, 28}, ids(t, &user_model.SearchUserOptions{Type: user_model.UserTypeIndividual, Asc: user_model.AscUserFilter{MemberOf: 3}}))
		assert.Equal(t, []int64{2, 4, 28}, ids(t, &user_model.SearchUserOptions{Type: user_model.UserTypeIndividual, Asc: user_model.AscUserFilter{MemberOf: 3, AllMembers: true}}))
		assert.Empty(t, ids(t, &user_model.SearchUserOptions{Type: user_model.UserTypeIndividual, Asc: user_model.AscUserFilter{MemberOf: -1}}))
	})

	t.Run("Most active counts only what anyone may see", func(t *testing.T) {
		_, err := e.Exec("DELETE FROM `action`")
		require.NoError(t, err)
		now := time.Now()
		act := func(actor int64, private bool, at time.Time) {
			_, err := e.Exec("INSERT INTO `action` (user_id, op_type, act_user_id, repo_id, is_private, created_unix) VALUES (?, 5, ?, 1, ?, ?)",
				actor, actor, private, at.Unix())
			require.NoError(t, err)
		}
		hourAgo := now.Add(-time.Hour)
		// user4: two public actions; user2: one public, five in private repositories and two from before the window;
		// user28: three, but keeps their activity private
		act(4, false, hourAgo)
		act(4, false, hourAgo)
		act(2, false, hourAgo)
		for range 5 {
			act(2, true, hourAgo)
		}
		act(2, false, now.AddDate(0, 0, -user_model.AscActiveDays-1))
		act(2, false, now.AddDate(0, 0, -user_model.AscActiveDays-1))
		for range 3 {
			act(28, false, hourAgo)
		}
		_, err = e.Exec("UPDATE `user` SET keep_activity_private = ? WHERE id = 28", true)
		require.NoError(t, err)
		// user2 also acted three times in repo3 of org3, a public repository of a limited organization: not counted
		_, err = e.Exec("UPDATE `user` SET visibility = ? WHERE id = 3", 1)
		require.NoError(t, err)
		_, err = e.Exec("UPDATE `repository` SET is_private = ? WHERE id = 3", false)
		require.NoError(t, err)
		for range 3 {
			_, err = e.Exec("INSERT INTO `action` (user_id, op_type, act_user_id, repo_id, is_private, created_unix) VALUES (2, 5, 2, 3, ?, ?)", false, hourAgo.Unix())
			require.NoError(t, err)
		}
		// a watcher's copy of an action of user2 does not count
		_, err = e.Exec("INSERT INTO `action` (user_id, op_type, act_user_id, repo_id, is_private, created_unix) VALUES (4, 5, 2, 1, ?, ?)", false, hourAgo.Unix())
		require.NoError(t, err)
		_, err = e.Exec("INSERT INTO `action` (user_id, op_type, act_user_id, repo_id, is_private, created_unix) VALUES (4, 5, 2, 1, ?, ?)", false, hourAgo.Unix())
		require.NoError(t, err)

		got := ids(t, &user_model.SearchUserOptions{
			Type:    user_model.UserTypeIndividual,
			OrderBy: user_model.AscMostActiveOrder(now),
			Asc:     user_model.AscUserFilter{MemberOf: 3, AllMembers: true},
		})
		assert.Equal(t, []int64{4, 2, 28}, got)
	})
}
