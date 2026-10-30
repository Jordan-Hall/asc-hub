// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package setting

import (
	"net/http"
	"strconv"
	"testing"

	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unittest"
	"forgejo.org/modules/templates"
	"forgejo.org/services/contexttest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// restoreAs posts the restore of a repository as the given user and returns the status.
func restoreAs(t *testing.T, userID, repoID int64) int {
	ctx, resp := contexttest.MockContext(t, "/user/settings/repo-trash/"+strconv.FormatInt(repoID, 10)+"/restore")
	ctx.Render = templates.HTMLRenderer()
	contexttest.LoadUser(t, ctx, userID)
	ctx.SetParams(":id", strconv.FormatInt(repoID, 10))
	RepoTrashRestore(ctx)
	return resp.Code
}

func TestRepoTrashRestore(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	// repository 1 belongs to user 2; repository 3 to organisation 3, which user 2 owns; user 4 owns neither
	for _, id := range []int64{1, 3} {
		repo := unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{ID: id})
		require.NoError(t, repo_model.TrashRepository(t.Context(), repo, 2))
	}

	// a stranger gets a 404, as for a repository that does not exist, and nothing is restored
	assert.Equal(t, http.StatusNotFound, restoreAs(t, 4, 1))
	assert.Equal(t, http.StatusNotFound, restoreAs(t, 4, 3))
	unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{ID: 1, Status: repo_model.RepositoryTrashed})
	unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{ID: 3, Status: repo_model.RepositoryTrashed})

	// a repository that is not in the trash is a 404 too, for its own owner
	assert.Equal(t, http.StatusNotFound, restoreAs(t, 2, 2))

	// the owner restores a repository of their own
	assert.Equal(t, http.StatusSeeOther, restoreAs(t, 2, 1))
	unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{ID: 1, Status: repo_model.RepositoryReady})

	// an owner of the organisation restores the organisation's repository
	assert.Equal(t, http.StatusSeeOther, restoreAs(t, 2, 3))
	unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{ID: 3, Status: repo_model.RepositoryReady})
}
