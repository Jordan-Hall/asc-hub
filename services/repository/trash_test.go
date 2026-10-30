// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repository

import (
	"testing"
	"time"

	"forgejo.org/models/db"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/test"
	"forgejo.org/modules/timeutil"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTrashOrDeleteRepository(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	owner := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2})

	t.Run("on an ASC hub the repository goes to the trash", func(t *testing.T) {
		defer test.MockVariableValue(&setting.Asc.Enabled, true)()
		repo := unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{ID: 1})
		require.NoError(t, TrashOrDeleteRepository(t.Context(), owner, repo, false))
		unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{ID: 1, Status: repo_model.RepositoryTrashed})
		_, err := repo_model.GetRepoTrash(t.Context(), 1)
		require.NoError(t, err)
		// it is not purged before its time
		require.NoError(t, PurgeTrashedRepositories(t.Context()))
		unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{ID: 1})

		// when its 30 days are over, the purge deletes it and its record
		_, err = db.GetEngine(t.Context()).Where("repo_id = ?", 1).Cols("purge_unix").Update(&repo_model.RepoTrash{PurgeUnix: timeutil.TimeStamp(time.Now().Add(-time.Hour).Unix())})
		require.NoError(t, err)
		require.NoError(t, PurgeTrashedRepositories(t.Context()))
		unittest.AssertNotExistsBean(t, &repo_model.Repository{ID: 1})
		_, err = repo_model.GetRepoTrash(t.Context(), 1)
		require.ErrorIs(t, err, repo_model.ErrNotTrashed)
	})

	t.Run("elsewhere it is deleted at once", func(t *testing.T) {
		defer test.MockVariableValue(&setting.Asc.Enabled, false)()
		repo := unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{ID: 4})
		require.NoError(t, TrashOrDeleteRepository(t.Context(), owner, repo, false))
		unittest.AssertNotExistsBean(t, &repo_model.Repository{ID: 4})
		_, err := repo_model.GetRepoTrash(t.Context(), 4)
		assert.ErrorIs(t, err, repo_model.ErrNotTrashed)
	})
}
