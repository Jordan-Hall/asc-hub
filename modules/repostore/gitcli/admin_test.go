// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package gitcli_test

import (
	"os"
	"path/filepath"
	"testing"

	"forgejo.org/modules/git"
	"forgejo.org/modules/repostore"
	"forgejo.org/modules/repostore/gitcli"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInit(t *testing.T) {
	w := gitcli.New()
	for _, format := range []string{"sha1", "sha256"} {
		if format == "sha256" && !git.SupportHashSha256 {
			continue
		}
		repo := repostore.RepoRef{Owner: "o", Name: "i", ObjectFormat: format, Path: filepath.Join(t.TempDir(), "i.git")}
		require.NoError(t, w.Init(t.Context(), repostore.InitRequest{Repo: repo, DefaultBranch: "trunk"}))
		assert.Equal(t, "refs/heads/trunk", gitOut(t, repo.Path, "symbolic-ref", "HEAD"))
		assert.Equal(t, format, gitOut(t, repo.Path, "rev-parse", "--show-object-format"))
		assert.Equal(t, "true", gitOut(t, repo.Path, "rev-parse", "--is-bare-repository"))
		assert.ErrorIs(t, w.Init(t.Context(), repostore.InitRequest{Repo: repo}), repostore.ErrExists)
	}

	t.Run("invalid", func(t *testing.T) {
		repo := repostore.RepoRef{Owner: "o", Name: "i", Path: filepath.Join(t.TempDir(), "i.git")}
		assert.ErrorIs(t, w.Init(t.Context(), repostore.InitRequest{Repo: repo}), repostore.ErrInvalidRequest, "no object format")
		repo.ObjectFormat = "sha1"
		assert.ErrorIs(t, w.Init(t.Context(), repostore.InitRequest{Repo: repo, DefaultBranch: "a b"}), repostore.ErrInvalidRequest)
		repo.Path = "relative.git"
		assert.ErrorIs(t, w.Init(t.Context(), repostore.InitRequest{Repo: repo}), repostore.ErrInvalidRequest)
		_, err := os.Stat("relative.git")
		assert.True(t, os.IsNotExist(err))
	})
}

func TestForkDeleteSize(t *testing.T) {
	w := gitcli.New()
	repo := newRepo(t, "src", "sha1")
	root := commit(t, repo, nil, "main", "root", put("a.txt", "a\n"))
	commit(t, repo, root, "other", "other", put("b.txt", "b\n"))

	fork := repostore.RepoRef{Owner: "o", Name: "fork", ObjectFormat: "sha1", Path: filepath.Join(t.TempDir(), "fork.git")}
	require.NoError(t, w.Fork(t.Context(), repostore.ForkRequest{Repo: fork, From: repo, SingleBranch: "main"}))
	assert.Equal(t, root.String(), id(t, fork, "refs/heads/main").String())
	assert.Empty(t, gitOut(t, fork.Path, "for-each-ref", "refs/heads/other"), "only the requested branch is copied")
	assert.ErrorIs(t, w.Fork(t.Context(), repostore.ForkRequest{Repo: fork, From: repo}), repostore.ErrExists)

	full := repostore.RepoRef{Owner: "o", Name: "full", ObjectFormat: "sha1", Path: filepath.Join(t.TempDir(), "full.git")}
	require.NoError(t, w.Fork(t.Context(), repostore.ForkRequest{Repo: full, From: repo}))
	assert.NotEmpty(t, gitOut(t, full.Path, "for-each-ref", "refs/heads/other"))

	size, err := w.Size(t.Context(), repo)
	require.NoError(t, err)
	assert.Positive(t, size.Logical)
	assert.Equal(t, size.Logical, size.Unique)

	missing := repostore.RepoRef{Owner: "o", Name: "none", Path: filepath.Join(t.TempDir(), "none.git")}
	size, err = w.Size(t.Context(), missing)
	require.NoError(t, err)
	assert.Zero(t, size.Logical)

	require.NoError(t, w.Delete(t.Context(), fork))
	_, err = os.Stat(fork.Path)
	assert.True(t, os.IsNotExist(err))
	require.NoError(t, w.Delete(t.Context(), fork), "deleting twice is not an error")

	t.Run("delete refuses a root", func(t *testing.T) {
		assert.Error(t, w.Delete(t.Context(), repostore.RepoRef{Name: "x", Path: "/"}))
		assert.Error(t, w.Delete(t.Context(), repostore.RepoRef{Name: "x", Path: "relative"}))
		keep := t.TempDir()
		assert.Error(t, w.Delete(t.Context(), repostore.RepoRef{Name: "x", Path: keep}), "not a repository directory")
		_, err := os.Stat(keep)
		assert.NoError(t, err)
	})
}

func TestUpdateRefs(t *testing.T) {
	w := gitcli.New()
	repo := newRepo(t, "u", "sha1")
	root := commit(t, repo, nil, "main", "root", put("a", "a\n"))
	next := commit(t, repo, root, "next", "next", put("a", "b\n"))

	t.Run("create, fast-forward and delete", func(t *testing.T) {
		res, err := w.UpdateRefs(t.Context(), repostore.UpdateRefsRequest{
			Repo: repo,
			Updates: []repostore.RefUpdateSpec{
				{Name: "refs/heads/created", NewRev: root.String()},
				{Name: "refs/heads/main", NewRev: "refs/heads/next"},
			},
		})
		require.NoError(t, err)
		require.Len(t, res, 2)
		assert.Nil(t, res[0].Old)
		assert.Equal(t, root.String(), res[0].New.String())
		assert.Equal(t, root.String(), res[1].Old.String())
		assert.Equal(t, next.String(), res[1].New.String())
		assert.Equal(t, next.String(), id(t, repo, "refs/heads/main").String())

		res, err = w.UpdateRefs(t.Context(), repostore.UpdateRefsRequest{
			Repo:    repo,
			Updates: []repostore.RefUpdateSpec{{Name: "refs/heads/created", Delete: true}},
		})
		require.NoError(t, err)
		assert.Equal(t, root.String(), res[0].Old.String())
		assert.Nil(t, res[0].New)
		assert.Empty(t, gitOut(t, repo.Path, "for-each-ref", "refs/heads/created"))
	})

	t.Run("non-fast-forward needs force", func(t *testing.T) {
		_, err := w.UpdateRefs(t.Context(), repostore.UpdateRefsRequest{
			Repo:    repo,
			Updates: []repostore.RefUpdateSpec{{Name: "refs/heads/main", NewRev: root.String()}},
		})
		var outOfDate *git.ErrPushOutOfDate
		require.ErrorAs(t, err, &outOfDate)
		assert.Equal(t, next.String(), id(t, repo, "refs/heads/main").String())

		_, err = w.UpdateRefs(t.Context(), repostore.UpdateRefsRequest{
			Repo:    repo,
			Updates: []repostore.RefUpdateSpec{{Name: "refs/heads/main", NewRev: root.String(), Force: true}},
		})
		require.NoError(t, err)
		assert.Equal(t, root.String(), id(t, repo, "refs/heads/main").String())
	})

	t.Run("all or nothing", func(t *testing.T) {
		before := id(t, repo, "refs/heads/main")
		_, err := w.UpdateRefs(t.Context(), repostore.UpdateRefsRequest{
			Repo: repo,
			Updates: []repostore.RefUpdateSpec{
				{Name: "refs/heads/atomic", NewRev: root.String()},
				{Name: "refs/heads/next", NewRev: root.String()}, // not fast-forward
			},
		})
		require.Error(t, err)
		assert.Empty(t, gitOut(t, repo.Path, "for-each-ref", "refs/heads/atomic"))
		assert.Equal(t, before.String(), id(t, repo, "refs/heads/main").String())
	})

	t.Run("from another repository", func(t *testing.T) {
		src := newRepo(t, "src", "sha1")
		srcRoot := commit(t, src, nil, "main", "root", put("s", "s\n"))
		res, err := w.UpdateRefs(t.Context(), repostore.UpdateRefsRequest{
			Repo: repo, From: &src,
			Updates: []repostore.RefUpdateSpec{{Name: "refs/heads/imported", NewRev: "refs/heads/main"}},
		})
		require.NoError(t, err)
		assert.Equal(t, srcRoot.String(), res[0].New.String())
		assert.True(t, hasObject(t, repo, srcRoot))
	})

	t.Run("refspec tricks are refused", func(t *testing.T) {
		for _, name := range []string{"refs/heads/a:refs/heads/b", "heads/a", "refs/heads/a b"} {
			_, err := w.UpdateRefs(t.Context(), repostore.UpdateRefsRequest{
				Repo:    repo,
				Updates: []repostore.RefUpdateSpec{{Name: name, NewRev: root.String()}},
			})
			require.ErrorIs(t, err, repostore.ErrInvalidRequest, name)
		}
	})
}
