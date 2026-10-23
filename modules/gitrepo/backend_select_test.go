// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package gitrepo

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"forgejo.org/modules/git"
	"forgejo.org/modules/git/ascbackend"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/test"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// backendRepo is a Repository that carries a storage_backend value, like the repository model.
type backendRepo struct {
	NamedRepository
	backend string
}

func (r backendRepo) GetStorageBackend() string { return r.backend }

func TestParseStorageBackend(t *testing.T) {
	for _, s := range []string{"git", "dual", "asc"} {
		got, err := ParseStorageBackend(s)
		require.NoError(t, err)
		assert.Equal(t, StorageBackend(s), got)
	}
	for _, s := range []string{"", "GIT", "Asc", " git", "git ", "gitcli", "none", "git\x00", strings.Repeat("x", 1000)} {
		_, err := ParseStorageBackend(s)
		require.ErrorIs(t, err, ErrUnknownStorageBackend, "%q", s)
		assert.Less(t, len(err.Error()), 200, "an error must not repeat a long stored value")
	}
}

func TestStorageBackendOfNamedRepository(t *testing.T) {
	got, err := StorageBackendOf(NamedRepository{Owner: "o", Name: "n"})
	require.NoError(t, err)
	assert.Equal(t, StorageBackendGit, got)
}

func TestSelectBackend(t *testing.T) {
	defer test.MockVariableValue(&setting.Asc.Enabled, false)()

	sel := func(backend string) (string, error) {
		return selectBackend(backendRepo{NamedRepository{"o", "n"}, backend})
	}

	t.Run("git needs no asc", func(t *testing.T) {
		name, err := sel("git")
		require.NoError(t, err)
		assert.Equal(t, git.DefaultBackendName, name)
	})
	t.Run("unknown fails closed", func(t *testing.T) {
		for _, b := range []string{"", "GIT", "svn", "asc;git"} {
			_, err := sel(b)
			require.ErrorIs(t, err, ErrUnknownStorageBackend, "%q", b)
		}
	})
	t.Run("dual and asc need asc enabled", func(t *testing.T) {
		for _, b := range []string{"dual", "asc"} {
			_, err := sel(b)
			require.ErrorIs(t, err, ErrAscDisabled, b)
		}
	})

	setting.Asc.Enabled = true
	t.Run("dual reads the git directory", func(t *testing.T) {
		name, err := sel("dual")
		require.NoError(t, err)
		assert.Equal(t, git.DefaultBackendName, name)
	})
	// gitrepo links in the asc backend (modules/git/ascbackend), so an asc repository never reads the git directory.
	require.Contains(t, git.Backends(), ascBackendName)
	require.Equal(t, ascbackend.Name, ascBackendName)
	t.Run("asc uses the asc backend, dual and git do not", func(t *testing.T) {
		name, err := sel("asc")
		require.NoError(t, err)
		assert.Equal(t, ascBackendName, name)
		for _, b := range []string{"git", "dual"} {
			name, err = sel(b)
			require.NoError(t, err)
			assert.Equal(t, git.DefaultBackendName, name, b)
		}
	})
	t.Run("an asc backend with no client is an error, not a git read", func(t *testing.T) {
		ascbackend.Reset()
		defer test.MockVariableValue(&setting.RepoRootPath, t.TempDir())()
		r := backendRepo{NamedRepository{"o", "n"}, "asc"}
		require.NoError(t, os.MkdirAll(repoPath(r), 0o755))
		repo, err := OpenRepository(context.Background(), r)
		require.ErrorIs(t, err, ascbackend.ErrNotConfigured)
		assert.Nil(t, repo)
	})
}

func TestOpenRepositoryRefusesUnknownBackend(t *testing.T) {
	defer test.MockVariableValue(&setting.RepoRootPath, t.TempDir())()
	r := backendRepo{NamedRepository{"o", "n"}, "bogus"}
	require.NoError(t, os.MkdirAll(filepath.Dir(repoPath(r)), 0o755))
	require.NoError(t, os.Mkdir(repoPath(r), 0o755))

	repo, err := OpenRepository(context.Background(), r)
	require.ErrorIs(t, err, ErrUnknownStorageBackend)
	assert.Nil(t, repo)
}
