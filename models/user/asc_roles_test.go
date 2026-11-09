// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package user_test

import (
	"testing"

	"forgejo.org/models/db"
	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTheRolesOfThePeopleDirectory(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	ctx := t.Context()
	e := db.GetEngine(ctx)
	_, err := e.Exec("UPDATE `user` SET type = ? WHERE id = 30", user_model.UserTypeBot)
	require.NoError(t, err)
	load := func(ids ...int64) []*user_model.User {
		out := make([]*user_model.User, 0, len(ids))
		for _, id := range ids {
			out = append(out, unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: id}))
		}
		return out
	}

	t.Run("A role is read from what the account can do", func(t *testing.T) {
		// user1 is a site admin, user2 can write to repositories of others, user5 cannot, user30 is a bot, org3 has no role
		roles, err := user_model.AscRolesOf(ctx, load(1, 2, 5, 30, 3))
		require.NoError(t, err)
		assert.Equal(t, map[int64]string{1: "owner", 2: "maintainer", 5: "member", 30: "agent"}, roles)
	})

	t.Run("The counts add up to the active accounts the viewer may see", func(t *testing.T) {
		total := func(cond string, args ...any) int64 {
			n, err := e.Table("user").Where("is_active = ? AND type IN (0, 4, 5)"+cond, append([]any{true}, args...)...).Count()
			require.NoError(t, err)
			return n
		}
		admin := load(1)[0]
		counts, err := user_model.AscRoleCounts(ctx, admin)
		require.NoError(t, err)
		assert.Equal(t, total(" AND is_admin = ? AND type <> 4", true), counts["owner"])
		assert.Equal(t, int64(1), counts["agent"])
		assert.Positive(t, counts["maintainer"])
		assert.Positive(t, counts["member"])
		assert.Equal(t, total(""), counts["owner"]+counts["maintainer"]+counts["member"]+counts["agent"])

		visitor, err := user_model.AscRoleCounts(ctx, nil)
		require.NoError(t, err)
		assert.Equal(t, total(" AND visibility = 0"), visitor["owner"]+visitor["maintainer"]+visitor["member"]+visitor["agent"])
		assert.Less(t, visitor["member"]+visitor["maintainer"], counts["member"]+counts["maintainer"], "private and limited accounts are left out")
	})
}
