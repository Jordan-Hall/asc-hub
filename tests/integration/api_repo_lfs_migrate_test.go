// Copyright 2021 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package integration

import (
	"net/http"
	"path"
	"testing"

	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/lfs"
	"forgejo.org/modules/setting"
	api "forgejo.org/modules/structs"
	migrations_allowlist "forgejo.org/services/migrations/allowlist"
	"forgejo.org/tests"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAPIRepoLFSMigrateLocal(t *testing.T) {
	defer tests.PrepareTestEnv(t)()

	oldImportLocalPaths := setting.ImportLocalPaths
	oldAllowLocalNetworks := setting.Migrations.AllowLocalNetworks
	setting.ImportLocalPaths = true
	setting.Migrations.AllowLocalNetworks = true
	require.NoError(t, migrations_allowlist.Init())

	user := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 1})

	req := NewRequestWithJSON(t, "POST", "/api/v1/repos/migrate", &api.MigrateRepoOptions{
		CloneAddr:   path.Join(setting.RepoRootPath, "migration/lfs-test.git"),
		RepoOwnerID: user.ID,
		RepoName:    "lfs-test-local",
		LFS:         true,
	}).AddBasicAuth(user.Name)
	resp := MakeRequest(t, req, NoExpectedStatus)
	assert.Equal(t, http.StatusCreated, resp.Code)

	store := lfs.NewContentStore()
	ok, _ := store.Verify(lfs.Pointer{Oid: "fb8f7d8435968c4f82a726a92395be4d16f2f63116caf36c8ad35c60831ab041", Size: 6})
	assert.True(t, ok)
	ok, _ = store.Verify(lfs.Pointer{Oid: "d6f175817f886ec6fbbc1515326465fa96c3bfd54a4ea06cfd6dbbd8340e0152", Size: 6})
	assert.True(t, ok)

	setting.ImportLocalPaths = oldImportLocalPaths
	setting.Migrations.AllowLocalNetworks = oldAllowLocalNetworks
	require.NoError(t, migrations_allowlist.Init()) // reset old migration settings
}

// LFS asked for on a server that does not run LFS is an error. It used to be dropped, and the repository came
// out with pointer files and no word about it.
func TestAPIRepoLFSMigrateWithoutLFSServerIsRefused(t *testing.T) {
	defer tests.PrepareTestEnv(t)()

	old, oldLocal, oldNet := setting.LFS.StartServer, setting.ImportLocalPaths, setting.Migrations.AllowLocalNetworks
	setting.LFS.StartServer, setting.ImportLocalPaths, setting.Migrations.AllowLocalNetworks = false, true, true
	require.NoError(t, migrations_allowlist.Init())
	defer func() {
		setting.LFS.StartServer, setting.ImportLocalPaths, setting.Migrations.AllowLocalNetworks = old, oldLocal, oldNet
		require.NoError(t, migrations_allowlist.Init())
	}()

	user := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 1})
	req := NewRequestWithJSON(t, "POST", "/api/v1/repos/migrate", &api.MigrateRepoOptions{
		CloneAddr:   path.Join(setting.RepoRootPath, "migration/lfs-test.git"),
		RepoOwnerID: user.ID,
		RepoName:    "lfs-without-server",
		LFS:         true,
	}).AddBasicAuth(user.Name)
	resp := MakeRequest(t, req, http.StatusUnprocessableEntity)
	assert.Contains(t, resp.Body.String(), "LFS_START_SERVER")
}
