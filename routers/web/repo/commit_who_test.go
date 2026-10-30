// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	"testing"
	"time"

	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/git"
	"forgejo.org/services/contexttest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFilterCommitsByAuthorKind(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	bot := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2})
	// make user 2's commit identity an agent for the test: the filter reads IsBot of the account behind the email
	bot.Type = user_model.UserTypeBot
	require.NoError(t, user_model.UpdateUserCols(t.Context(), bot, "type"))
	person := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 4})

	mk := func(id, email string) *git.Commit {
		sig := &git.Signature{Name: "n", Email: email, When: time.Now()}
		return &git.Commit{ID: git.MustIDFromString(id), Author: sig, Committer: sig}
	}
	agentC := mk("1111111111111111111111111111111111111111", bot.Email)
	personC := mk("2222222222222222222222222222222222222222", person.Email)
	strangerC := mk("3333333333333333333333333333333333333333", "nobody@example.org")

	ctx, _ := contexttest.MockContext(t, "/user2/repo1/commits/branch/master?who=agents")
	all := []*git.Commit{agentC, personC, strangerC}
	agents := filterCommitsByAuthorKind(ctx, all, true)
	people := filterCommitsByAuthorKind(ctx, all, false)
	require.Len(t, agents, 1)
	assert.Equal(t, agentC.ID.String(), agents[0].ID.String())
	require.Len(t, people, 2, "an author without an account is a person")
	assert.Equal(t, personC.ID.String(), people[0].ID.String())
	assert.Equal(t, strangerC.ID.String(), people[1].ID.String())
}
