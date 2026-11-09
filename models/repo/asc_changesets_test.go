// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo_test

import (
	"testing"

	"forgejo.org/models/db"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unittest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The changeset count of a repository is stored once, replaced on a new head, and read back for a list of rows.
func TestAscRepoChangesetsAreKeptPerRepository(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	ctx := db.DefaultContext

	got, err := repo_model.GetAscRepoChangesets(ctx, nil)
	require.NoError(t, err)
	assert.Empty(t, got)

	require.NoError(t, repo_model.SetAscRepoChangesets(ctx, 1, "aaa", 3))
	require.NoError(t, repo_model.SetAscRepoChangesets(ctx, 2, "bbb", 7))
	require.NoError(t, repo_model.SetAscRepoChangesets(ctx, 1, "ccc", 5))

	got, err = repo_model.GetAscRepoChangesets(ctx, []int64{1, 2, 3})
	require.NoError(t, err)
	require.Len(t, got, 2, "repository 3 was never counted")
	assert.Equal(t, "ccc", got[1].CommitSha)
	assert.EqualValues(t, 5, got[1].Changesets)
	assert.Equal(t, "bbb", got[2].CommitSha)
	assert.EqualValues(t, 7, got[2].Changesets)
	assert.Positive(t, got[1].UpdatedUnix)
	unittest.AssertCount(t, &repo_model.AscRepoChangesets{RepoID: 1}, 1)
}
