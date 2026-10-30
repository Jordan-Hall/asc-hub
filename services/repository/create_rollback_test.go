// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package repository

import (
	"os"
	"path/filepath"
	"testing"

	"forgejo.org/models/db"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A repository that cannot be created because files are in the way (the directory of another repository) loses its new
// database rows and nothing else: the files at that path are not this call's to delete.
func TestCreateRepositoryOverExistingFilesKeepsThem(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	user := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2})

	path := repo_model.RepoPath(user.Name, "someone-elses")
	require.NoError(t, os.MkdirAll(path, 0o755))
	marker := filepath.Join(path, "precious.txt")
	require.NoError(t, os.WriteFile(marker, []byte("not yours"), 0o644))

	_, err := CreateRepositoryDirectly(t.Context(), user, user, CreateRepoOptions{Name: "someone-elses"})
	require.Error(t, err)
	assert.True(t, repo_model.IsErrRepoFilesAlreadyExist(err), "%v", err)
	content, rerr := os.ReadFile(marker)
	require.NoError(t, rerr, "the files at the path are intact")
	assert.Equal(t, "not yours", string(content))
	exist, err := repo_model.IsRepositoryModelExist(t.Context(), user, "someone-elses")
	require.NoError(t, err)
	assert.False(t, exist, "no row of the failed repository is left")
}

// A failure after this call made the store removes the store and the rows.
func TestCreateRepositoryFailureAfterInitRemovesWhatItMade(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	user := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2})

	_, err := CreateRepositoryDirectly(t.Context(), user, user, CreateRepoOptions{Name: "fails-after-init", AutoInit: true, License: "NoSuchLicense"})
	require.Error(t, err)
	assert.NoDirExists(t, repo_model.RepoPath(user.Name, "fails-after-init"), "the directory this call made is removed")
	exist, err := repo_model.IsRepositoryModelExist(t.Context(), user, "fails-after-init")
	require.NoError(t, err)
	assert.False(t, exist)
}

// initRepository reports whether it made the store: over an existing one it did not.
func TestInitRepositoryOverAnExistingStoreIsNotCreated(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	repo := unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{ID: 1})
	require.DirExists(t, repo.RepoPath())
	created, err := initRepository(t.Context(), repo.RepoPath(), unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2}), repo, CreateRepoOptions{ObjectFormatName: "sha1"})
	require.Error(t, err)
	assert.False(t, created)
	assert.DirExists(t, repo.RepoPath())
}

// KeepFiles deletes the rows of a repository and leaves its files.
func TestDeleteRepositoryKeepFiles(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	repo := unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{ID: 1})
	require.DirExists(t, repo.RepoPath())

	require.NoError(t, DeleteRepositoryDirectly(t.Context(), repo.ID, DeleteRepositoryOpts{KeepFiles: true}))
	assert.DirExists(t, repo.RepoPath(), "the files stay")
	has, err := db.GetEngine(t.Context()).ID(repo.ID).Exist(&repo_model.Repository{})
	require.NoError(t, err)
	assert.False(t, has, "the row is gone")
	units, err := db.GetEngine(t.Context()).Where("repo_id = ?", repo.ID).Count(&repo_model.RepoUnit{})
	require.NoError(t, err)
	assert.Zero(t, units, "no unit rows are left")
}
