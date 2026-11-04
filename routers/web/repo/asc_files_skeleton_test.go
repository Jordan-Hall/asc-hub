// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	"strings"
	"testing"

	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unittest"
	"forgejo.org/modules/git"
	"forgejo.org/modules/gitrepo"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// While the last change of each file loads, every column of a row holds a skeleton, so the row keeps its size.
func TestFileListSkeletonKeepsTheRow(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	repo := unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{ID: 1})
	gitRepo, err := gitrepo.OpenRepository(t.Context(), repo)
	require.NoError(t, err)
	defer gitRepo.Close()
	commit, err := gitRepo.GetBranchCommit("master")
	require.NoError(t, err)
	entries, err := commit.Tree.ListEntries()
	require.NoError(t, err)
	require.NotEmpty(t, entries)

	list := func(withCommit bool) string {
		return renderPart(t, "repo/view_list", func(d map[string]any) {
			files := make([]git.CommitInfo, 0, len(entries))
			for _, e := range entries {
				info := git.CommitInfo{Entry: e}
				if withCommit {
					info.Commit = commit
				}
				files = append(files, info)
			}
			d["Files"] = files
			d["HasFilesWithoutLatestCommit"] = !withCommit
			d["LastCommitLoaderURL"] = "/user2/repo1/lastcommit/x/"
		})
	}

	loading := list(false)
	assert.Contains(t, loading, `hx-get="/user2/repo1/lastcommit/x/"`)
	assert.Equal(t, len(entries), strings.Count(loading, "rp-sk rp-sk-line"), "the message column")
	assert.Equal(t, 2*len(entries), strings.Count(loading, "rp-sk rp-sk-word"), "the author and age columns")

	loaded := list(true)
	assert.NotContains(t, loaded, "rp-sk-line")
	assert.NotContains(t, loaded, "rp-sk-word")
	assert.NotContains(t, loaded, "hx-get")
}
