// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repository

import (
	"testing"

	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unittest"
	"forgejo.org/modules/gitrepo"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadSharedBranches(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	repo := unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{ID: 1})
	gitRepo, err := gitrepo.OpenRepository(t.Context(), repo)
	require.NoError(t, err)
	defer gitRepo.Close()

	names := func(shared []string, keyword string) (string, []string, int64) {
		t.Helper()
		def, branches, n, err := LoadSharedBranches(t.Context(), repo, gitRepo, shared, keyword, 1, 50)
		require.NoError(t, err)
		var got []string
		for _, b := range branches {
			got = append(got, b.DBBranch.Name)
		}
		return def.DBBranch.Name, got, n
	}

	def, got, n := names([]string{"refs/heads/master", "refs/heads/branch2", "branch2", "refs/heads/no-such"}, "")
	assert.Equal(t, "master", def)
	assert.Equal(t, []string{"branch2"}, got, "full and short names, each once; the default branch apart")
	assert.EqualValues(t, 1, n)

	_, got, n = names([]string{"refs/heads/master"}, "")
	assert.Empty(t, got, "only the default branch is shared")
	assert.Zero(t, n)

	_, got, _ = names([]string{"refs/heads/branch2"}, "nothing-like-it")
	assert.Empty(t, got)
}
