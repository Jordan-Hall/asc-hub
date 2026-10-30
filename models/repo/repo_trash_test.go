// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo_test

import (
	"testing"
	"time"

	"forgejo.org/models/db"
	perm_model "forgejo.org/models/perm"
	access_model "forgejo.org/models/perm/access"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTrashAndRestore(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	repo := unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{ID: 1})
	owner := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: repo.OwnerID})
	wasPrivate := repo.IsPrivate

	// the owner reads it before
	perm, err := access_model.GetUserRepoPermission(t.Context(), repo, owner)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, perm.AccessMode, perm_model.AccessModeRead)

	require.NoError(t, repo_model.TrashRepository(t.Context(), repo, owner.ID))
	trashed := unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{ID: 1})
	assert.True(t, trashed.IsTrashed())
	assert.True(t, trashed.IsPrivate)

	// closed to everyone: the owner, a site administrator and an anonymous visitor
	admin := unittest.AssertExistsAndLoadBean(t, &user_model.User{IsAdmin: true})
	for _, who := range []*user_model.User{owner, admin, nil} {
		perm, err := access_model.GetUserRepoPermission(t.Context(), trashed, who)
		require.NoError(t, err)
		assert.Equal(t, perm_model.AccessModeNone, perm.AccessMode)
		assert.False(t, perm.HasAnyUnitAccess())
	}

	// it is in no list
	list, _, err := repo_model.SearchRepository(t.Context(), &repo_model.SearchRepoOptions{ListOptions: db.ListOptions{PageSize: 100, Page: 1}, Private: true, OwnerID: owner.ID, Actor: owner})
	require.NoError(t, err)
	for _, r := range list {
		assert.NotEqual(t, int64(1), r.ID)
	}

	rec, err := repo_model.GetRepoTrash(t.Context(), 1)
	require.NoError(t, err)
	assert.Equal(t, owner.ID, rec.DeletedByID)
	assert.WithinDuration(t, time.Now().AddDate(0, 0, repo_model.TrashDays), rec.PurgeUnix.AsTime(), time.Minute)
	due, err := repo_model.FindTrashDue(t.Context(), time.Now(), 10)
	require.NoError(t, err)
	assert.Empty(t, due, "nothing is due for 30 days")
	due, err = repo_model.FindTrashDue(t.Context(), time.Now().AddDate(0, 0, repo_model.TrashDays+1), 10)
	require.NoError(t, err)
	assert.Len(t, due, 1)

	// restore gives the repository and its visibility back
	require.NoError(t, repo_model.RestoreRepository(t.Context(), 1))
	back := unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{ID: 1})
	assert.False(t, back.IsTrashed())
	assert.Equal(t, wasPrivate, back.IsPrivate)
	_, err = repo_model.GetRepoTrash(t.Context(), 1)
	require.ErrorIs(t, err, repo_model.ErrNotTrashed)
	perm, err = access_model.GetUserRepoPermission(t.Context(), back, owner)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, perm.AccessMode, perm_model.AccessModeRead)
}
