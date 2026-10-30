// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	"testing"

	issues_model "forgejo.org/models/issues"
	"forgejo.org/models/unittest"
	"forgejo.org/services/contexttest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The pull request that was opened for the change of an agent run says so, and links to the run for whoever may read
// runs.
func TestPrepareAscRunPull(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	setStorageBackend(t, 1, "asc")
	pull := unittest.AssertExistsAndLoadBean(t, &issues_model.PullRequest{ID: 2})

	t.Run("a pull request of a person says nothing", func(t *testing.T) {
		ctx, _ := contexttest.MockContext(t, "user2/repo1/pulls/2")
		prepareAscRunPull(ctx, pull)
		assert.Nil(t, ctx.Data["AscRunPull"])
	})

	require.NoError(t, issues_model.SetPullRequestAscRun(t.Context(), pull, 7, "coder", "user2"))

	t.Run("without the agents area the run is named and not linked", func(t *testing.T) {
		ctx, _ := contexttest.MockContext(t, "user2/repo1/pulls/2")
		contexttest.LoadRepo(t, ctx, 1)
		prepareAscRunPull(ctx, pull)
		got, ok := ctx.Data["AscRunPull"].(*AscRunPull)
		require.True(t, ok)
		assert.EqualValues(t, 7, got.RunID)
		assert.Equal(t, "coder", got.Agent)
		assert.Equal(t, "user2", got.StartedBy)
		assert.Empty(t, got.URL, "a visitor has no agents area")
	})

	t.Run("nil is none", func(t *testing.T) {
		ctx, _ := contexttest.MockContext(t, "user2/repo1/pulls/2")
		prepareAscRunPull(ctx, nil)
		assert.Nil(t, ctx.Data["AscRunPull"])
	})
}
