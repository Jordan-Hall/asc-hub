// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package organization_test

import (
	"testing"

	"forgejo.org/models/organization"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unittest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A repository in the trash is in no list that an organisation member or a team reads.
func TestTrashedRepositoryIsInNoOrgList(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	repo := unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{ID: 3}) // owned by organisation 3
	has := func(list repo_model.RepositoryList) bool {
		for _, r := range list {
			if r.ID == 3 {
				return true
			}
		}
		return false
	}
	orgRepos, err := organization.GetOrgRepositories(t.Context(), repo.OwnerID)
	require.NoError(t, err)
	assert.True(t, has(orgRepos))
	teamRepos, err := organization.GetTeamRepositories(t.Context(), &organization.SearchTeamRepoOptions{TeamID: 1})
	require.NoError(t, err)

	require.NoError(t, repo_model.TrashRepository(t.Context(), repo, 2))
	orgRepos, err = organization.GetOrgRepositories(t.Context(), repo.OwnerID)
	require.NoError(t, err)
	assert.False(t, has(orgRepos))
	teamRepos, err = organization.GetTeamRepositories(t.Context(), &organization.SearchTeamRepoOptions{TeamID: 1})
	require.NoError(t, err)
	assert.False(t, has(teamRepos))
}
