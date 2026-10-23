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

func TestCreateCommitSpecialEntries(t *testing.T) {
	w := gitcli.New()
	repo := newRepo(t, "g", "sha1")
	sub := strings.Repeat("ab", 20)

	root := commit(t, repo, nil, "main", "special",
		repostore.FileOp{Kind: repostore.OpPut, Path: "vendor/lib", Mode: git.EntryModeCommit, Content: strings.NewReader(sub + "\n")},
		repostore.FileOp{Kind: repostore.OpPut, Path: "link", Mode: git.EntryModeSymlink, Content: strings.NewReader("vendor/lib")},
		put("--test", "dash"),
		put("with,comma.txt", "comma"),
	)
	assert.Contains(t, gitOut(t, repo.Path, "ls-tree", root.String(), "vendor/lib"), "160000 commit "+sub)
	assert.Contains(t, gitOut(t, repo.Path, "ls-tree", root.String(), "link"), "120000 blob")
	assert.Equal(t, "dash", fileAt(t, repo, "main", "--test"))
	assert.Equal(t, "comma", fileAt(t, repo, "main", "with,comma.txt"))

	t.Run("a submodule needs a full commit id", func(t *testing.T) {
		for _, content := range []string{"", "abc", sub + "00", "not a commit id", strings.Repeat("a", 500)} {
			_, err := w.CreateCommit(t.Context(), repostore.CommitRequest{
				Repo: repo, Parent: root, Meta: meta("x"),
				Ops: []repostore.FileOp{{Kind: repostore.OpPut, Path: "bad", Mode: git.EntryModeCommit, Content: strings.NewReader(content)}},
			})
			require.ErrorIs(t, err, repostore.ErrInvalidRequest, content)
		}
	})

	t.Run("a delete after a put in one request", func(t *testing.T) {
		res, err := w.CreateCommit(t.Context(), repostore.CommitRequest{
			Repo: repo, Parent: root, Meta: meta("x"), Update: toBranch(repo, "ops"),
			Ops: []repostore.FileOp{put("new.txt", "n"), del("new.txt"), del("link"), put("again.txt", "a")},
		})
		require.NoError(t, err)
		out := gitOut(t, repo.Path, "ls-tree", "-r", "--name-only", res.Tree.String())
		assert.NotContains(t, out, "new.txt")
		assert.NotContains(t, out, "link")
		assert.Contains(t, out, "again.txt")
	})
}
