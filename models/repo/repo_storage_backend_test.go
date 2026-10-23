// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo_test

import (
	"testing"

	"forgejo.org/models/db"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unittest"
	"forgejo.org/modules/gitrepo"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/test"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRepositoryStorageBackend(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())

	t.Run("existing rows are git", func(t *testing.T) {
		repo := unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{ID: 1})
		assert.Equal(t, "git", repo.StorageBackend)
		assert.Equal(t, "git", repo.GetStorageBackend())
	})

	t.Run("an inserted repository without a value is git", func(t *testing.T) {
		repo := &repo_model.Repository{OwnerID: 2, OwnerName: "user2", Name: "sbtest", LowerName: "sbtest"}
		require.NoError(t, db.Insert(t.Context(), repo))
		loaded := unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{ID: repo.ID})
		assert.Equal(t, "git", loaded.StorageBackend)
	})

	t.Run("a stored value survives a round trip", func(t *testing.T) {
		repo := &repo_model.Repository{OwnerID: 2, OwnerName: "user2", Name: "sbtest2", LowerName: "sbtest2", StorageBackend: "dual"}
		require.NoError(t, db.Insert(t.Context(), repo))
		loaded := unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{ID: repo.ID})
		assert.Equal(t, "dual", loaded.GetStorageBackend())
	})

	t.Run("an in-memory repository without a value is git", func(t *testing.T) {
		assert.Equal(t, "git", (&repo_model.Repository{}).GetStorageBackend())
	})
}

func TestOpenRepositoryFollowsStorageBackend(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	repo := unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{ID: 1})

	open := func(backend string) error {
		r := *repo
		r.StorageBackend = backend
		gitRepo, err := gitrepo.OpenRepository(t.Context(), &r)
		if err == nil {
			require.NoError(t, gitRepo.Close())
		}
		return err
	}

	defer test.MockVariableValue(&setting.Asc.Enabled, false)()
	require.NoError(t, open("git"))
	require.NoError(t, open(""))
	require.ErrorIs(t, open("dual"), gitrepo.ErrAscDisabled)
	require.ErrorIs(t, open("asc"), gitrepo.ErrAscDisabled)
	require.ErrorIs(t, open("bogus"), gitrepo.ErrUnknownStorageBackend)

	setting.Asc.Enabled = true
	require.NoError(t, open("dual"))
	require.NoError(t, open("asc"))
	require.ErrorIs(t, open("bogus"), gitrepo.ErrUnknownStorageBackend)
}
