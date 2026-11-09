// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo_test

import (
	"testing"
	"time"

	"forgejo.org/models/db"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTheQuickChipsOfTheRepositoryLists(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	ctx := t.Context()
	e := db.GetEngine(ctx)
	now := time.Now()

	ids := func(t *testing.T, q repo_model.AscQuickFilter) []int64 {
		t.Helper()
		repos, _, err := repo_model.SearchRepository(ctx, &repo_model.SearchRepoOptions{
			ListOptions: db.ListOptions{Page: 1, PageSize: 100},
			AllPublic:   true,
			OrderBy:     db.SearchOrderByID,
			AscQuick:    q,
		})
		require.NoError(t, err)
		out := make([]int64, 0, len(repos))
		for _, r := range repos {
			out = append(out, r.ID)
		}
		return out
	}

	t.Run("Has open stacks keeps the repositories with an open pull request", func(t *testing.T) {
		// only pull request 2 (issue 3, repository 1) stays open; a closed one does not count
		_, err := e.Exec("UPDATE `issue` SET is_closed = ? WHERE is_pull = ?", true, true)
		require.NoError(t, err)
		_, err = e.Exec("UPDATE `issue` SET is_closed = ? WHERE id = 3", false)
		require.NoError(t, err)
		assert.Equal(t, []int64{1}, ids(t, repo_model.AscQuickFilter{OpenStacks: true}))
	})

	t.Run("Agent activity keeps the repositories where a bot account acted this week", func(t *testing.T) {
		_, err := e.Exec("UPDATE `user` SET type = ? WHERE id = 30", user_model.UserTypeBot)
		require.NoError(t, err)
		_, err = e.Exec("DELETE FROM `action`")
		require.NoError(t, err)
		insert := func(actor, repo int64, at time.Time) {
			_, err := e.Exec("INSERT INTO `action` (user_id, op_type, act_user_id, repo_id, is_private, created_unix) VALUES (?, 1, ?, ?, ?, ?)",
				actor, actor, repo, false, at.Unix())
			require.NoError(t, err)
		}
		insert(30, 1, now.Add(-time.Hour))   // an agent, this week
		insert(30, 4, now.AddDate(0, 0, -8)) // an agent, eight days ago
		insert(2, 10, now.Add(-time.Hour))   // a person, this week
		assert.Equal(t, []int64{1}, ids(t, repo_model.AscQuickFilter{AgentsSince: repo_model.AgentActivitySince(now)}))
	})

	t.Run("Starred keeps what the viewer starred", func(t *testing.T) {
		// user2 starred repositories 2 (private) and 4 (public)
		assert.Equal(t, []int64{4}, ids(t, repo_model.AscQuickFilter{StarredBy: 2}))
	})

	t.Run("The chips add to each other", func(t *testing.T) {
		assert.Empty(t, ids(t, repo_model.AscQuickFilter{OpenStacks: true, StarredBy: 2}))
		assert.Equal(t, []int64{1}, ids(t, repo_model.AscQuickFilter{OpenStacks: true, AgentsSince: repo_model.AgentActivitySince(now)}))
	})
}
