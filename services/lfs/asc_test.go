// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package lfs

import (
	"testing"

	repo_model "forgejo.org/models/repo"
	"forgejo.org/modules/setting"

	"github.com/stretchr/testify/assert"
)

func TestAscRepositoriesHaveNoLFS(t *testing.T) {
	for backend, refused := range map[string]bool{"": false, "git": false, "dual": false, "asc": true} {
		assert.Equal(t, refused, refusesLFS(&repo_model.Repository{StorageBackend: backend}), "storage backend %q", backend)
	}
	assert.False(t, refusesLFS(nil), "a repository that does not exist is left to the handlers")
}

func TestLFSAllowed(t *testing.T) {
	old := setting.LFS.StartServer
	t.Cleanup(func() { setting.LFS.StartServer = old })

	setting.LFS.StartServer = true
	assert.True(t, (&repo_model.Repository{}).LFSAllowed())
	assert.True(t, (&repo_model.Repository{StorageBackend: "dual"}).LFSAllowed())
	assert.False(t, (&repo_model.Repository{StorageBackend: "asc"}).LFSAllowed())

	setting.LFS.StartServer = false
	assert.False(t, (&repo_model.Repository{}).LFSAllowed())
}
