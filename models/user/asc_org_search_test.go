// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package user_test

import (
	"sort"
	"testing"
	"time"

	"forgejo.org/models/db"
	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/structs"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTheFiltersOfTheOrganizationDirectory(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	ctx := t.Context()
	e := db.GetEngine(ctx)
	exec := func(q string, args ...any) {
		t.Helper()
		_, err := e.Exec(append([]any{q}, args...)...)
		require.NoError(t, err)
	}
	admin := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 1})
	user5 := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 5})
	search := func(viewer *user_model.User, f user_model.AscOrgFilter, order db.SearchOrderBy) []int64 {
		t.Helper()
		f.Viewer = viewer
		if order == "" {
			order = db.SearchOrderByID
		}
		users, _, err := user_model.SearchUsers(ctx, &user_model.SearchUserOptions{
			Actor:       viewer,
			Type:        user_model.UserTypeOrganization,
			ListOptions: db.ListOptions{PageSize: 100},
			Visible:     []structs.VisibleType{structs.VisibleTypePublic, structs.VisibleTypeLimited, structs.VisibleTypePrivate},
			OrderBy:     order,
			AscOrg:      f,
		})
		require.NoError(t, err)
		ids := make([]int64, 0, len(users))
		for _, u := range users {
			ids = append(ids, u.ID)
		}
		return ids
	}
	sorted := func(ids []int64) []int64 {
		sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
		return ids
	}

	t.Run("Verified keeps the organizations that proved a domain", func(t *testing.T) {
		exec("INSERT INTO `asc_org_domain` (org_id, domain, token, verified_unix, checked_unix) VALUES (17, 'example.org', 't1', ?, ?)", time.Now().Unix(), time.Now().Unix())
		exec("INSERT INTO `asc_org_domain` (org_id, domain, token, verified_unix, checked_unix) VALUES (19, 'example.net', 't2', 0, 0)")
		assert.Equal(t, []int64{17}, search(nil, user_model.AscOrgFilter{Verified: true}, ""))
	})

	t.Run("Has agents counts only the memberships the viewer may see", func(t *testing.T) {
		exec("UPDATE `user` SET type = ? WHERE id = 30", user_model.UserTypeBot)
		exec("INSERT INTO `org_user` (uid, org_id, is_public) VALUES (30, 19, ?)", true)
		exec("INSERT INTO `org_user` (uid, org_id, is_public) VALUES (30, 6, ?)", false)
		assert.Equal(t, []int64{19}, search(nil, user_model.AscOrgFilter{WithAgents: true}, ""), "a visitor sees public memberships only")
		assert.Equal(t, []int64{6, 19}, search(user5, user_model.AscOrgFilter{WithAgents: true}, ""), "a member of org6 sees its private memberships")
		assert.Equal(t, []int64{6, 19}, search(admin, user_model.AscOrgFilter{WithAgents: true}, ""))
		user8 := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 8})
		assert.Equal(t, []int64{19}, search(user8, user_model.AscOrgFilter{WithAgents: true}, ""))
	})

	t.Run("I am a member keeps the organizations of the viewer", func(t *testing.T) {
		assert.Equal(t, []int64{6, 7, 23, 36}, sorted(search(user5, user_model.AscOrgFilter{ViewerMember: true}, "")))
		assert.Empty(t, search(nil, user_model.AscOrgFilter{ViewerMember: true}, ""), "a visitor is a member of nothing")
	})

	t.Run("Size keeps the organizations by their number of members", func(t *testing.T) {
		exec("UPDATE `user` SET num_members = 0 WHERE type = ?", user_model.UserTypeOrganization)
		exec("UPDATE `user` SET num_members = 10 WHERE id = 3")
		exec("UPDATE `user` SET num_members = 11 WHERE id = 6")
		exec("UPDATE `user` SET num_members = 100 WHERE id = 7")
		exec("UPDATE `user` SET num_members = 101 WHERE id = 17")
		exec("UPDATE `user` SET num_members = 1 WHERE id = 19")
		assert.Equal(t, []int64{3, 19}, search(admin, user_model.AscOrgFilter{Size: user_model.AscOrgSizeSmall}, ""))
		assert.Equal(t, []int64{6, 7}, search(admin, user_model.AscOrgFilter{Size: user_model.AscOrgSizeMedium}, ""))
		assert.Equal(t, []int64{17}, search(admin, user_model.AscOrgFilter{Size: user_model.AscOrgSizeLarge}, ""))
		assert.Equal(t, user_model.AscOrgSizeAny, user_model.ParseAscOrgSize("huge"))
		assert.Equal(t, user_model.AscOrgSizeMedium, user_model.ParseAscOrgSize("medium"))
		assert.Equal(t, []int64{17}, search(admin, user_model.AscOrgFilter{Size: user_model.AscOrgSizeLarge, Verified: true}, ""), "the filters add up")
	})

	t.Run("Most active counts only what anyone may see", func(t *testing.T) {
		now := time.Now()
		act := func(actor, repoID int64, private bool) {
			exec("INSERT INTO `action` (user_id, act_user_id, op_type, repo_id, is_private, created_unix) VALUES (?, ?, 6, ?, ?, ?)",
				actor, actor, repoID, private, now.Add(-time.Hour).Unix())
		}
		act(5, 27, false) // org19, public repo27
		act(5, 27, false)
		act(5, 23, false) // org17, public repo23
		act(5, 24, false) // org17, private repo24: not counted
		act(5, 24, false)
		act(5, 23, true) // a private action: not counted
		act(5, 23, true)
		act(5, 40, false) // privated_org: not counted
		act(5, 40, false)
		act(5, 40, false)
		exec("UPDATE `user` SET keep_activity_private = ? WHERE id = 8", true)
		act(8, 23, false) // a person who keeps activity private: not counted
		act(8, 23, false)
		ids := search(admin, user_model.AscOrgFilter{}, user_model.AscMostActiveOrgOrder(now))
		require.GreaterOrEqual(t, len(ids), 3)
		assert.Equal(t, []int64{19, 17}, ids[:2])
		rest := make([]string, 0, len(ids)-2)
		for _, id := range ids[2:] {
			rest = append(rest, unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: id}).Name)
		}
		assert.IsNonDecreasing(t, rest, "the rest has no public action and goes by name")
	})
}
