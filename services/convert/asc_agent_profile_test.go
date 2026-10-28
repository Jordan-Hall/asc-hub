// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package convert

import (
	"testing"

	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAgentsOnlySeeTheProfileOfThoseWhoAllowIt(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	ctx := t.Context()
	person := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2})
	person.FullName, person.Description, person.Location, person.Website = "User Two", "bio", "Lisbon", "https://example.org"
	agent := user_model.NewActionsUser()
	other := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 4})

	// the default is that agents may read
	assert.True(t, user_model.AscAgentsMayReadProfile(ctx, person.ID))
	got := ToUser(ctx, person, agent)
	assert.Equal(t, "User Two", got.FullName)
	assert.Equal(t, "bio", got.Description)

	require.NoError(t, user_model.SetAscAgentsMayReadProfile(ctx, person.ID, false))
	got = ToUser(ctx, person, agent)
	assert.Equal(t, person.Name, got.UserName, "who the person is stays")
	assert.Empty(t, got.FullName)
	assert.Empty(t, got.Description)
	assert.Empty(t, got.Location)
	assert.Empty(t, got.Website)
	assert.Empty(t, got.Pronouns)
	assert.Equal(t, person.GetPlaceholderEmail(), got.Email, "never the real address")

	// people are not agents: the switch does not hide the profile from them, nor from the person
	assert.Equal(t, "User Two", ToUser(ctx, person, other).FullName)
	assert.Equal(t, "User Two", ToUser(ctx, person, person).FullName)
	assert.Equal(t, "User Two", ToUser(ctx, person, nil).FullName)

	require.NoError(t, user_model.SetAscAgentsMayReadProfile(ctx, person.ID, true))
	assert.Equal(t, "User Two", ToUser(ctx, person, agent).FullName)
}
