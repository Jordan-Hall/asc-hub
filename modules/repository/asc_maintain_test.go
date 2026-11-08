// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repository

import (
	"testing"

	"forgejo.org/models/db"
	perm_model "forgejo.org/models/perm"
	access_model "forgejo.org/models/perm/access"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAscMaintain(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	ctx := db.DefaultContext
	// collaboration fixture 2: user 4 writes to repository 4
	repo := unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{ID: 4})

	is, err := repo_model.IsAscMaintainer(ctx, 4, 4)
	require.NoError(t, err)
	assert.False(t, is)

	require.NoError(t, SetAscMaintain(ctx, repo, 4))
	is, err = repo_model.IsAscMaintainer(ctx, 4, 4)
	require.NoError(t, err)
	assert.True(t, is)
	user := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 4})
	p, err := access_model.GetUserRepoPermission(ctx, repo, user)
	require.NoError(t, err)
	assert.Equal(t, perm_model.AccessModeWrite, p.AccessMode, "Maintain is write access to the code")

	// not a collaborator: nothing happens
	require.NoError(t, SetAscMaintain(ctx, repo, 8))
	unittest.AssertNotExistsBean(t, &repo_model.Collaboration{RepoID: 4, UserID: 8})

	// write again makes a plain writer; another mode too
	require.NoError(t, ChangeCollaborationAccessMode(ctx, repo, 4, perm_model.AccessModeWrite))
	is, err = repo_model.IsAscMaintainer(ctx, 4, 4)
	require.NoError(t, err)
	assert.False(t, is)
	require.NoError(t, SetAscMaintain(ctx, repo, 4))
	require.NoError(t, ChangeCollaborationAccessMode(ctx, repo, 4, perm_model.AccessModeAdmin))
	c := unittest.AssertExistsAndLoadBean(t, &repo_model.Collaboration{RepoID: 4, UserID: 4})
	assert.False(t, c.AscMaintain)
	assert.Equal(t, perm_model.AccessModeAdmin, c.Mode)

	// a mark left on a collaboration that is not write is no Maintain role
	_, err = db.GetEngine(ctx).ID(c.ID).Cols("asc_maintain").Update(&repo_model.Collaboration{AscMaintain: true})
	require.NoError(t, err)
	is, err = repo_model.IsAscMaintainer(ctx, 4, 4)
	require.NoError(t, err)
	assert.False(t, is)
	is, err = repo_model.IsAscMaintainer(ctx, 0, 4)
	require.NoError(t, err)
	assert.False(t, is)
}
