// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package gitcli_test

import (
	"strings"
	"testing"

	"forgejo.org/modules/git"
	"forgejo.org/modules/repostore"
	"forgejo.org/modules/repostore/gitcli"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestApplyPatch(t *testing.T) {
	w := gitcli.New()
	repo := newRepo(t, "p", "sha1")
	root := commit(t, repo, nil, "main", "root", put("a.txt", "one\ntwo\nthree\n"), put("gone.txt", "bye\n"))
	changed := commit(t, repo, root, "changed", "changed", put("a.txt", "one\n2\nthree\n"), del("gone.txt"), put("new.txt", "new\n"))
	patch := gitOut(t, repo.Path, "diff", "--binary", root.String(), changed.String()) + "\n"

	t.Run("applies", func(t *testing.T) {
		res, err := w.ApplyPatch(t.Context(), repostore.PatchRequest{
			Repo: repo, Parent: root, Patch: patch, Meta: meta("patched"), Update: toBranch(repo, "main"),
		})
		require.NoError(t, err)
		assert.Equal(t, res.Commit.String(), id(t, repo, "refs/heads/main").String())
		assert.Equal(t, gitOut(t, repo.Path, "rev-parse", changed.String()+"^{tree}"), res.Tree.String())
		assert.Equal(t, []string{root.String()}, parents(t, repo, "main"))
		assert.Equal(t, "patched", gitOut(t, repo.Path, "log", "-1", "--format=%B", "main"))
	})

	t.Run("a patch that does not apply publishes nothing", func(t *testing.T) {
		before := id(t, repo, "refs/heads/main")
		_, err := w.ApplyPatch(t.Context(), repostore.PatchRequest{
			Repo: repo, Parent: before, Meta: meta("bad"), Update: toBranch(repo, "main"),
			Patch: "diff --git a/missing.txt b/missing.txt\n--- a/missing.txt\n+++ b/missing.txt\n@@ -1 +1 @@\n-x\n+y\n",
		})
		require.Error(t, err)
		assert.Equal(t, before.String(), id(t, repo, "refs/heads/main").String())
	})

	t.Run("a patch cannot write outside the tree", func(t *testing.T) {
		before := id(t, repo, "refs/heads/main")
		_, err := w.ApplyPatch(t.Context(), repostore.PatchRequest{
			Repo: repo, Parent: before, Meta: meta("bad"), Update: toBranch(repo, "main"),
			Patch: "diff --git a/../../evil b/../../evil\nnew file mode 100644\n--- /dev/null\n+++ b/../../evil\n@@ -0,0 +1 @@\n+x\n",
		})
		require.Error(t, err)
		assert.Equal(t, before.String(), id(t, repo, "refs/heads/main").String())
	})
}

func TestCherryPickAndRevert(t *testing.T) {
	for _, mergeTree := range []bool{true, false} {
		name := "merge-tree"
		if !mergeTree {
			name = "read-tree"
		}
		t.Run(name, func(t *testing.T) {
			if mergeTree && !git.SupportGitMergeTree {
				t.Skip("git without merge-tree --write-tree")
			}
			saved := git.SupportGitMergeTree
			git.SupportGitMergeTree = mergeTree
			defer func() { git.SupportGitMergeTree = saved }()

			w := gitcli.New()
			repo := newRepo(t, "c", "sha1")
			root := commit(t, repo, nil, "main", "root", put("a.txt", "a\n"), put("b.txt", "b\n"))
			feature := commit(t, repo, root, "feature", "feature", put("b.txt", "b feature\n"), put("f.txt", "f\n"))
			main2 := commit(t, repo, root, "main", "main2", put("a.txt", "a main\n"))

			t.Run("cherry-pick", func(t *testing.T) {
				res, err := w.CherryPick(t.Context(), repostore.PickRequest{
					Repo: repo, Parent: main2, Commit: feature, Meta: meta("picked"), Update: toBranch(repo, "main"),
				})
				require.NoError(t, err)
				assert.Equal(t, res.Commit.String(), id(t, repo, "refs/heads/main").String())
				assert.Equal(t, []string{main2.String()}, parents(t, repo, "main"))
				assert.Equal(t, "a main", fileAt(t, repo, "main", "a.txt"))
				assert.Equal(t, "b feature", fileAt(t, repo, "main", "b.txt"))
				assert.Equal(t, "f", fileAt(t, repo, "main", "f.txt"))
			})

			t.Run("revert", func(t *testing.T) {
				tip := id(t, repo, "refs/heads/main")
				res, err := w.Revert(t.Context(), repostore.PickRequest{
					Repo: repo, Parent: tip, Commit: feature, Meta: meta("reverted"), Update: toBranch(repo, "reverted"),
				})
				require.NoError(t, err)
				assert.Equal(t, res.Commit.String(), id(t, repo, "refs/heads/reverted").String())
				assert.Equal(t, "b", fileAt(t, repo, "reverted", "b.txt"))
				requireNoFile(t, repo, "reverted", "f.txt")
				assert.Equal(t, "a main", fileAt(t, repo, "reverted", "a.txt"))
			})

			t.Run("conflict", func(t *testing.T) {
				clash := commit(t, repo, root, "clash", "clash", put("a.txt", "a clash\n"))
				before := id(t, repo, "refs/heads/main")
				_, err := w.CherryPick(t.Context(), repostore.PickRequest{
					Repo: repo, Parent: before, Commit: clash, Meta: meta("never"), Update: toBranch(repo, "main"),
				})
				var conflict *repostore.ConflictError
				require.ErrorAs(t, err, &conflict)
				assert.Equal(t, []string{"a.txt"}, conflict.Files)
				assert.Equal(t, before.String(), id(t, repo, "refs/heads/main").String())
			})

			t.Run("a root commit picks onto its empty tree", func(t *testing.T) {
				if mergeTree {
					t.Skip("git merge-tree cannot take a tree as the merge base")
				}
				other := newRepo(t, "o", "sha1")
				first := commit(t, other, nil, "main", "first", put("x", "x\n"))
				second := commit(t, other, first, "main", "second", put("y", "y\n"))
				res, err := w.CherryPick(t.Context(), repostore.PickRequest{
					Repo: other, Parent: second, Commit: first, Meta: meta("again"), Update: toBranch(other, "picked"),
				})
				require.NoError(t, err)
				assert.Equal(t, "x", fileAt(t, other, "picked", "x"))
				assert.Equal(t, "y", fileAt(t, other, "picked", "y"))
				assert.NotNil(t, res.Commit)
			})

			t.Run("a missing commit is an error", func(t *testing.T) {
				_, err := w.CherryPick(t.Context(), repostore.PickRequest{
					Repo: repo, Parent: main2, Commit: git.MustIDFromString(strings.Repeat("a", 40)), Meta: meta("x"),
				})
				require.Error(t, err)
			})
		})
	}
}
