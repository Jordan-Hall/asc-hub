// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package pull

import (
	"testing"

	issues_model "forgejo.org/models/issues"
	access_model "forgejo.org/models/perm/access"
	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// An agent never merges, even with the permission that lets a person merge.
func TestAnAgentIsNotAllowedToMerge(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	pr := unittest.AssertExistsAndLoadBean(t, &issues_model.PullRequest{ID: 2})
	require.NoError(t, pr.LoadBaseRepo(t.Context()))
	person := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2})
	perm, err := access_model.GetUserRepoPermission(t.Context(), pr.BaseRepo, person)
	require.NoError(t, err)

	ok, err := IsUserAllowedToMerge(t.Context(), pr, perm, person)
	require.NoError(t, err)
	assert.True(t, ok, "the owner merges")

	agent := *person
	agent.Type = user_model.UserTypeBot
	ok, err = IsUserAllowedToMerge(t.Context(), pr, perm, &agent)
	require.NoError(t, err)
	assert.False(t, ok, "the same permission does not let an agent merge")

	err = CheckPullMergeable(t.Context(), &agent, &perm, pr, MergeCheckTypeGeneral, true, "")
	assert.ErrorIs(t, err, ErrUserNotAllowedToMerge)
}
