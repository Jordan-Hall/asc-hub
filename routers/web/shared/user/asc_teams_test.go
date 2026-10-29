// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package user

import (
	"testing"

	"forgejo.org/models/db"
	"forgejo.org/models/organization"
	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/structs"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProfileTeamsListOnlyTeamsOfOrganizationsTheViewerSees(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	person := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2})
	org := unittest.AssertExistsAndLoadBean(t, &organization.Organization{ID: 3})
	viewer := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 4})

	teams, err := AscProfileTeams(db.DefaultContext, person, []*organization.Organization{org}, nil)
	require.NoError(t, err)
	require.NotEmpty(t, teams, "a public organization shows its teams to a visitor")
	assert.Contains(t, teams[0].Link, "/org3/teams/")

	limited := *org
	limited.Visibility = structs.VisibleTypeLimited
	teams, err = AscProfileTeams(db.DefaultContext, person, []*organization.Organization{&limited}, nil)
	require.NoError(t, err)
	assert.Empty(t, teams, "a limited organization is hidden from a visitor")
	teams, err = AscProfileTeams(db.DefaultContext, person, []*organization.Organization{&limited}, viewer)
	require.NoError(t, err)
	assert.NotEmpty(t, teams, "and shown to a signed in person")

	private := *org
	private.Visibility = structs.VisibleTypePrivate
	teams, err = AscProfileTeams(db.DefaultContext, person, []*organization.Organization{&private}, viewer)
	require.NoError(t, err)
	assert.Empty(t, teams, "a private organization is hidden from another person")
	teams, err = AscProfileTeams(db.DefaultContext, person, []*organization.Organization{&private}, person)
	require.NoError(t, err)
	assert.NotEmpty(t, teams, "but not from the person themself")
}
