// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package user

import (
	"testing"

	"forgejo.org/models/db"
	"forgejo.org/models/organization"
	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func teamsOf(t *testing.T, person, viewer *user_model.User, org *organization.Organization) []AscProfileTeam {
	t.Helper()
	teams, err := AscProfileTeams(db.DefaultContext, person, []*organization.Organization{org}, viewer)
	require.NoError(t, err)
	return teams
}

func TestProfileTeamsAreForTheMembersOfTheOrganization(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	person := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2}) // a public member of the public organization 3
	org := unittest.AssertExistsAndLoadBean(t, &organization.Organization{ID: 3})
	member := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 28})  // another member of organization 3
	outsider := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 5}) // not a member of organization 3
	admin := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 1})
	isMember, err := organization.IsOrganizationMember(db.DefaultContext, org.ID, outsider.ID)
	require.NoError(t, err)
	require.False(t, isMember, "the fixture user 5 is not in organization 3")

	assert.Empty(t, teamsOf(t, person, nil, org), "a visitor who is not signed in sees no team, also of a public organization")
	assert.Empty(t, teamsOf(t, person, outsider, org), "a person outside the organization sees no team")
	if teams := teamsOf(t, person, member, org); assert.NotEmpty(t, teams, "a member of the organization sees the teams") {
		assert.Contains(t, teams[0].Link, "/org3/teams/")
	}
	assert.NotEmpty(t, teamsOf(t, person, person, org), "the person sees their own teams")
	assert.NotEmpty(t, teamsOf(t, person, admin, org), "a site administrator sees them")
}

func TestProfileTeamsOfAPrivateMemberStayHidden(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	org := unittest.AssertExistsAndLoadBean(t, &organization.Organization{ID: 3})
	hidden := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 4}) // member of organization 3 who keeps it private
	outsider := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 5})
	public, err := organization.IsPublicMembership(db.DefaultContext, org.ID, hidden.ID)
	require.NoError(t, err)
	require.False(t, public, "the fixture user 4 keeps the membership private")
	assert.Empty(t, teamsOf(t, hidden, outsider, org))
	assert.Empty(t, teamsOf(t, hidden, nil, org))
}
