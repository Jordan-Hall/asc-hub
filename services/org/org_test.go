// Copyright 2021 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package org

import (
	"testing"

	"forgejo.org/models"
	"forgejo.org/models/actions"
	"forgejo.org/models/db"
	"forgejo.org/models/organization"
	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/optional"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMain(m *testing.M) {
	unittest.MainTest(m)
}

func TestDeleteOrganization(t *testing.T) {
	defer unittest.OverrideFixtures("services/org/TestDeleteOrganization")()
	require.NoError(t, unittest.PrepareTestDatabase())

	org := unittest.AssertExistsAndLoadBean(t, &organization.Organization{ID: 6})
	require.NoError(t, DeleteOrganization(db.DefaultContext, org, false))
	unittest.AssertNotExistsBean(t, &organization.Organization{ID: 6})
	unittest.AssertNotExistsBean(t, &organization.OrgUser{OrgID: 6})
	unittest.AssertNotExistsBean(t, &organization.Team{OrgID: 6})
	unittest.AssertNotExistsBean(t, &actions.ActionRunnerToken{OwnerID: optional.Some[int64](6)})
	unittest.AssertNotExistsBean(t, &user_model.Follow{FollowID: 6})
	unittest.AssertNotExistsBean(t, &user_model.BlockedUser{UserID: 6})

	org = unittest.AssertExistsAndLoadBean(t, &organization.Organization{ID: 3})
	err := DeleteOrganization(db.DefaultContext, org, false)
	require.Error(t, err)
	assert.True(t, models.IsErrUserOwnRepos(err))

	user := unittest.AssertExistsAndLoadBean(t, &organization.Organization{ID: 5})
	require.Error(t, DeleteOrganization(db.DefaultContext, user, false))
	unittest.CheckConsistencyFor(t, &user_model.User{}, &organization.Team{})

	assert.Zero(t, unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 1001}).NumFollowing)
}

func TestTransferOwnership(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	o := unittest.AssertExistsAndLoadBean(t, &organization.Organization{ID: 3})
	owners, err := o.GetOwnerTeam(db.DefaultContext)
	require.NoError(t, err)
	var owner int64
	members, err := organization.GetTeamMembers(db.DefaultContext, &organization.SearchMembersOptions{TeamID: owners.ID})
	require.NoError(t, err)
	require.NotEmpty(t, members)
	owner = members[0].ID

	// user4 is a member of the organization (team1) but not an owner; user1 is not a member at all
	isOwner := func(uid int64) bool {
		ok, err := o.IsOwnedBy(db.DefaultContext, uid)
		require.NoError(t, err)
		return ok
	}
	require.False(t, isOwner(4))

	require.ErrorIs(t, TransferOwnership(db.DefaultContext, o, 4, 4, false), ErrTransferNotOwner, "only an owner can hand ownership on")
	require.ErrorIs(t, TransferOwnership(db.DefaultContext, o, owner, 1, false), ErrTransferNotMember)
	require.ErrorIs(t, TransferOwnership(db.DefaultContext, o, owner, owner, false), ErrTransferAlreadyOwner)

	require.NoError(t, TransferOwnership(db.DefaultContext, o, owner, 4, false))
	assert.True(t, isOwner(4))
	assert.True(t, isOwner(owner), "without leave the giver stays an owner")

	// with leave the giver is out and the new owner remains: the organization is never without an owner
	require.NoError(t, models.AddTeamMember(db.DefaultContext, unittest.AssertExistsAndLoadBean(t, &organization.Team{OrgID: o.ID, LowerName: "team1"}), 5))
	require.NoError(t, TransferOwnership(db.DefaultContext, o, 4, 5, true))
	assert.True(t, isOwner(5))
	assert.False(t, isOwner(4), "the giver left the owners")
}
