// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repoquick

import (
	"testing"

	"forgejo.org/models/db"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/test"
	"forgejo.org/services/contexttest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func loadChangesets(t *testing.T, doer *user_model.User, repos repo_model.RepositoryList) map[int64]int64 {
	t.Helper()
	ctx, _ := contexttest.MockContext(t, "/explore/repos")
	ctx.Doer = doer
	ctx.IsSigned = doer != nil
	LoadChangesets(ctx, repos)
	counts, _ := ctx.Data["AscRowChangesets"].(map[int64]int64)
	return counts
}

// A row shows the stored changeset count of its default branch; a missing or older count is queued to be counted.
func TestTheRowsGetTheirChangesetCount(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	var queued []int64
	defer test.MockVariableValue(&QueueChangesetCount, func(r *repo_model.Repository) error {
		queued = append(queued, r.ID)
		return nil
	})()
	took := func() []int64 {
		out := queued
		queued = nil
		return out
	}
	const head1 = "65f1bf27bc3bf70f64657658635e66094edbcb4d"
	user2 := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2})
	var repos repo_model.RepositoryList
	for _, id := range []int64{1, 2, 4} {
		repos = append(repos, unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{ID: id}))
	}

	t.Run("Never counted: no count, and the rows with a head are queued", func(t *testing.T) {
		assert.Empty(t, loadChangesets(t, user2, repos))
		assert.ElementsMatch(t, []int64{1, 4}, took(), "repo2 has no branch row for its default branch")
	})

	require.NoError(t, repo_model.SetAscRepoChangesets(db.DefaultContext, 1, head1, 3812))
	require.NoError(t, repo_model.SetAscRepoChangesets(db.DefaultContext, 2, "older", 12))

	t.Run("A current count is shown and not queued; an older one is shown and queued", func(t *testing.T) {
		assert.Equal(t, map[int64]int64{1: 3812, 2: 12}, loadChangesets(t, user2, repos[:2]))
		assert.Empty(t, took(), "repo2 has no head to compare with")
		require.NoError(t, repo_model.SetAscRepoChangesets(db.DefaultContext, 1, "older", 3800))
		assert.Equal(t, map[int64]int64{1: 3800}, loadChangesets(t, user2, repos[:1]))
		assert.Equal(t, []int64{1}, took())
		require.NoError(t, repo_model.SetAscRepoChangesets(db.DefaultContext, 1, head1, 3812))
	})

	t.Run("A private repository the viewer cannot read has no count", func(t *testing.T) {
		assert.Equal(t, map[int64]int64{1: 3812}, loadChangesets(t, nil, repos[:2]))
		assert.Empty(t, took())
	})

	t.Run("An empty repository has no count", func(t *testing.T) {
		empty := *repos[0]
		empty.IsEmpty = true
		assert.Nil(t, loadChangesets(t, user2, repo_model.RepositoryList{&empty}))
		assert.Empty(t, took())
	})
}
