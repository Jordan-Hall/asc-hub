// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package actions

import (
	"testing"

	"forgejo.org/models/db"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unit"
	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The grant needs the owner and the repository to say yes. The owner of a user's repository never limits it, an
// organization does, and the default of an organization is no.
func TestPackagesWriteAllowed(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())

	userRepo := unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{ID: 1}) // user2/repo1
	orgRepo := unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{ID: 3})  // org3/repo3
	for _, repoID := range []int64{userRepo.ID, orgRepo.ID} {
		require.NoError(t, db.Insert(t.Context(), &repo_model.RepoUnit{RepoID: repoID, Type: unit.TypeActions, Config: &repo_model.ActionsConfig{}}))
	}
	t.Cleanup(func() {
		_, err := db.GetEngine(db.DefaultContext).Where("repo_id IN (1, 3)").Delete(&repo_model.RepoUnit{})
		require.NoError(t, err)
		_, err = db.GetEngine(db.DefaultContext).Where("user_id = 3").Delete(&user_model.Setting{})
		require.NoError(t, err)
	})
	setRepo := func(t *testing.T, repo *repo_model.Repository, allow bool) {
		t.Helper()
		cfgUnit, err := repo.GetUnit(t.Context(), unit.TypeActions)
		require.NoError(t, err)
		cfgUnit.ActionsConfig().AllowPackagesWrite = allow
		require.NoError(t, repo_model.UpdateRepoUnit(t.Context(), cfgUnit))
	}
	setOrg := func(t *testing.T, allow bool) {
		t.Helper()
		org := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 3})
		require.NoError(t, SetOrgAllowsPackagesWrite(t.Context(), org, allow))
	}

	t.Run("a user's repository", func(t *testing.T) {
		setRepo(t, userRepo, false)
		assert.False(t, PackagesWriteAllowed(t.Context(), userRepo))
		setRepo(t, userRepo, true)
		assert.True(t, PackagesWriteAllowed(t.Context(), userRepo))
	})

	t.Run("an organization's repository", func(t *testing.T) {
		setRepo(t, orgRepo, true)
		assert.False(t, PackagesWriteAllowed(t.Context(), orgRepo), "the default of an organization is no")

		setOrg(t, true)
		assert.True(t, PackagesWriteAllowed(t.Context(), orgRepo))

		setRepo(t, orgRepo, false)
		assert.False(t, PackagesWriteAllowed(t.Context(), orgRepo), "the repository narrows what the organization allows")

		setRepo(t, orgRepo, true)
		setOrg(t, false)
		assert.False(t, PackagesWriteAllowed(t.Context(), orgRepo), "the organization takes it back")
	})

	t.Run("a repository without the Actions unit", func(t *testing.T) {
		other := unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{ID: 2})
		assert.False(t, PackagesWriteAllowed(t.Context(), other))
	})

	t.Run("an owner that does not exist", func(t *testing.T) {
		assert.False(t, PackagesWriteAllowed(t.Context(), &repo_model.Repository{ID: 1, OwnerID: 999999}))
	})
}
