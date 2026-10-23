// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package ascbackend_test

import (
	"context"
	"testing"

	"forgejo.org/modules/git"
	"forgejo.org/modules/git/ascbackend"
	"forgejo.org/modules/gitrepo"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/test"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ascRepo is a repository model that has storage_backend = asc.
type ascRepo struct {
	gitrepo.NamedRepository
	backend string
}

func (r ascRepo) GetStorageBackend() string { return r.backend }

// A repository opened the way Forgejo opens it (gitrepo.OpenRepository) reads through asc, and the
// high-level git.Repository calls work on top of the backend.
func TestOpenedThroughGitrepo(t *testing.T) {
	fx := newFixture(t, ascbackend.Options{})
	defer test.MockVariableValue(&setting.RepoRootPath, fx.root)()
	defer test.MockVariableValue(&setting.Asc.Enabled, true)()
	ascbackend.Configure(fx.client, ascbackend.Options{Auth: func(ctx context.Context) (context.Context, error) { return ctx, nil }})
	t.Cleanup(ascbackend.Reset)

	repo, err := gitrepo.OpenRepository(t.Context(), ascRepo{gitrepo.NamedRepository{Owner: "Owner", Name: "Repo"}, "asc"})
	require.NoError(t, err)
	t.Cleanup(func() { _ = repo.Close() })

	be, err := repo.Backend()
	require.NoError(t, err)
	_, isAsc := be.(*ascbackend.Backend)
	require.True(t, isAsc, "%T", be)

	commit, err := repo.GetCommit("master")
	require.NoError(t, err)
	assert.Equal(t, fx.id("c4").String(), commit.ID.String())
	assert.Equal(t, "tweak main", commit.Summary())

	entries, err := commit.Tree.ListEntries()
	require.NoError(t, err)
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	assert.Contains(t, names, "main.go")
	assert.Contains(t, names, "dir")

	sub, err := commit.Tree.SubTree("dir/sub")
	require.NoError(t, err)
	subEntries, err := sub.ListEntries()
	require.NoError(t, err)
	require.Len(t, subEntries, 1)
	assert.Equal(t, "c.txt", subEntries[0].Name())

	content, err := commit.GetFileContent("dir/b.txt", 100)
	require.NoError(t, err)
	assert.Equal(t, "bravo\n", content)

	count, err := commit.CommitsCount()
	require.NoError(t, err)
	assert.Equal(t, int64(6), count)

	// The same repository model with git selects the git directory, and dual does too.
	gitRepo, err := gitrepo.OpenRepository(t.Context(), ascRepo{gitrepo.NamedRepository{Owner: "owner", Name: "repo"}, "git"})
	require.NoError(t, err)
	defer gitRepo.Close()
	gitBe, err := gitRepo.Backend()
	require.NoError(t, err)
	_, isAsc = gitBe.(*ascbackend.Backend)
	assert.False(t, isAsc)
}

// With no client configured, an asc repository cannot be opened, and nothing reads the git directory.
func TestGitrepoRefusesACgRepositoryWithoutAClient(t *testing.T) {
	fx := newFixture(t, ascbackend.Options{})
	defer test.MockVariableValue(&setting.RepoRootPath, fx.root)()
	defer test.MockVariableValue(&setting.Asc.Enabled, true)()
	ascbackend.Reset()

	repo, err := gitrepo.OpenRepository(t.Context(), ascRepo{gitrepo.NamedRepository{Owner: "owner", Name: "repo"}, "asc"})
	assert.ErrorIs(t, err, ascbackend.ErrNotConfigured)
	assert.Nil(t, repo)

	// A name that is not owner/repo.git below the repository root is refused too.
	ascbackend.Configure(fx.client, ascbackend.Options{})
	t.Cleanup(ascbackend.Reset)
	_, err = git.OpenRepositoryWithBackend(t.Context(), fx.root, ascbackend.Name)
	require.Error(t, err)
	_, err = git.OpenRepositoryWithBackend(t.Context(), fx.dir+"/..", ascbackend.Name)
	require.Error(t, err)
}
