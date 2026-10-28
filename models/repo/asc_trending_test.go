// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo_test

import (
	"testing"

	"forgejo.org/models/db"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAscTrendingKeepsOnlyWhatTheViewerMayRead(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	ctx := t.Context()

	public := unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{ID: 1})
	private := unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{ID: 2})
	require.False(t, public.IsPrivate)
	require.True(t, private.IsPrivate)

	// stars this week on both; the private repository has more
	for uid := int64(1); uid <= 3; uid++ {
		require.NoError(t, db.Insert(ctx, &repo_model.Star{UID: 100 + uid, RepoID: private.ID}))
	}
	require.NoError(t, db.Insert(ctx, &repo_model.Star{UID: 100, RepoID: public.ID}))
	_, err := db.GetEngine(ctx).Exec("UPDATE `star` SET created_unix = ?", 1) // and one very old star
	require.NoError(t, err)
	require.NoError(t, db.Insert(ctx, &repo_model.Star{UID: 200, RepoID: public.ID}))
	require.NoError(t, db.Insert(ctx, &repo_model.Star{UID: 201, RepoID: public.ID}))

	require.NoError(t, repo_model.RefreshAscTrending(ctx))
	rows, repos, err := repo_model.ListAscTrending(ctx, nil, 10)
	require.NoError(t, err)
	require.Len(t, rows, 1, "the private repository is never scored and the old star does not count")
	assert.Equal(t, public.ID, repos[0].ID)
	assert.EqualValues(t, 2, rows[0].Stars)
	assert.EqualValues(t, 6, rows[0].Score)

	// a signed-in viewer sees the same public repository
	viewer := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 4})
	rows, _, err = repo_model.ListAscTrending(ctx, viewer, 10)
	require.NoError(t, err)
	assert.Len(t, rows, 1)
}
