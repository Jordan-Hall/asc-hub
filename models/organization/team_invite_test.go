// Copyright 2022 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package organization_test

import (
	"testing"

	"forgejo.org/models/db"
	"forgejo.org/models/organization"
	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTeamInvite(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())

	team := unittest.AssertExistsAndLoadBean(t, &organization.Team{ID: 2})

	t.Run("MailExistsInTeam", func(t *testing.T) {
		user2 := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2})

		// user 2 already added to team 2, should result in error
		_, err := organization.CreateTeamInviteByEmail(db.DefaultContext, user2, team, user2.Email)
		require.Error(t, err)
	})

	t.Run("UserExistsInTeam", func(t *testing.T) {
		user2 := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2})
		user4 := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2})

		// user 4 already added to team 2, should result in error
		_, err := organization.CreateTeamInviteForUser(db.DefaultContext, user2, user4, team)
		require.Error(t, err)
		invited, err := organization.IsInvitedToOrganization(db.DefaultContext, team.OrgID, user4.ID)
		require.NoError(t, err)
		require.False(t, invited)
	})

	t.Run("CreateAndRemoveByUser", func(t *testing.T) {
		user1 := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 1})
		user5 := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 5})

		invite, err := organization.CreateTeamInviteForUser(db.DefaultContext, user1, user5, team)
		assert.NotNil(t, invite)
		require.NoError(t, err)
		invited, err := organization.IsInvitedToOrganization(db.DefaultContext, team.OrgID, user5.ID)
		require.NoError(t, err)
		require.True(t, invited)

		// Shouldn't allow duplicate invite by email
		_, err = organization.CreateTeamInviteByEmail(db.DefaultContext, user1, team, user5.Email)
		require.Error(t, err)
		// Shouldn't allow duplicate invite by user
		_, err = organization.CreateTeamInviteForUser(db.DefaultContext, user1, user5, team)
		require.Error(t, err)

		// should remove invite
		require.NoError(t, organization.RemoveInviteByID(db.DefaultContext, invite.ID, invite.TeamID))

		// invite should not exist
		_, err = organization.GetInviteByToken(db.DefaultContext, invite.Token)
		require.Error(t, err)
	})

	t.Run("CreateByEmailAndRemove", func(t *testing.T) {
		user1 := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 1})

		invite, err := organization.CreateTeamInviteByEmail(db.DefaultContext, user1, team, "org3@example.com")
		assert.NotNil(t, invite)
		require.NoError(t, err)

		// Shouldn't allow duplicate invite
		_, err = organization.CreateTeamInviteByEmail(db.DefaultContext, user1, team, "org3@example.com")
		require.Error(t, err)

		// should remove invite
		require.NoError(t, organization.RemoveInviteByID(db.DefaultContext, invite.ID, invite.TeamID))

		// invite should not exist
		_, err = organization.GetInviteByToken(db.DefaultContext, invite.Token)
		require.Error(t, err)
	})

	t.Run("CreateByUserAndRemove", func(t *testing.T) {
		user1 := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 1})

		invite, err := organization.CreateTeamInviteByEmail(db.DefaultContext, user1, team, "org3@example.com")
		assert.NotNil(t, invite)
		require.NoError(t, err)

		// Shouldn't allow duplicate invite
		_, err = organization.CreateTeamInviteByEmail(db.DefaultContext, user1, team, "org3@example.com")
		require.Error(t, err)

		// should remove invite
		require.NoError(t, organization.RemoveInviteByID(db.DefaultContext, invite.ID, invite.TeamID))

		// invite should not exist
		_, err = organization.GetInviteByToken(db.DefaultContext, invite.Token)
		require.Error(t, err)
	})
}

func TestPendingInvitesForUser(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	team := unittest.AssertExistsAndLoadBean(t, &organization.Team{ID: 3})
	doer := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 5})
	user4 := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 4}) // user4@example.com, confirmed
	stranger := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2})

	// made for the account, made for a confirmed address (other case), made for an address nobody has confirmed, made for someone else
	forAccount, err := organization.CreateTeamInviteForUser(db.DefaultContext, doer, user4, team)
	require.NoError(t, err)
	byMail, err := organization.CreateTeamInviteByEmail(db.DefaultContext, doer, team, "User4@Example.com")
	require.NoError(t, err)
	_, err = organization.CreateTeamInviteByEmail(db.DefaultContext, doer, team, "nobody-has-confirmed-this@example.com")
	require.NoError(t, err)
	_, err = organization.CreateTeamInviteForUser(db.DefaultContext, doer, stranger, team)
	require.NoError(t, err)

	got, err := organization.GetPendingInvitesForUser(db.DefaultContext, user4)
	require.NoError(t, err)
	var tokens []string
	for _, inv := range got {
		tokens = append(tokens, inv.Token)
	}
	assert.ElementsMatch(t, []string{forAccount.Token, byMail.Token}, tokens, "the account's and the confirmed address's, nothing else")

	// declining: only the person's own invitation, and it is gone afterwards
	assert.True(t, organization.IsErrTeamInviteNotFound(organization.DeclineInviteForUser(db.DefaultContext, stranger, byMail.Token)), "not theirs: it looks as if it did not exist")
	require.NoError(t, organization.DeclineInviteForUser(db.DefaultContext, user4, byMail.Token))
	got, err = organization.GetPendingInvitesForUser(db.DefaultContext, user4)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, forAccount.Token, got[0].Token)
	assert.True(t, organization.IsErrTeamInviteNotFound(organization.DeclineInviteForUser(db.DefaultContext, user4, byMail.Token)))

	// a person who is in the team already is not asked again
	require.NoError(t, db.Insert(db.DefaultContext, &organization.TeamUser{OrgID: team.OrgID, TeamID: team.ID, UID: user4.ID}))
	got, err = organization.GetPendingInvitesForUser(db.DefaultContext, user4)
	require.NoError(t, err)
	assert.Empty(t, got)
}
