// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package issues_test

import (
	"testing"

	issues_model "forgejo.org/models/issues"
	"forgejo.org/models/unittest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPullRequestAscRun(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	pr := unittest.AssertExistsAndLoadBean(t, &issues_model.PullRequest{ID: 1})
	other := unittest.AssertExistsAndLoadBean(t, &issues_model.PullRequest{ID: 2})

	got, err := issues_model.GetPullRequestAscRun(t.Context(), pr.ID)
	require.NoError(t, err)
	assert.Nil(t, got, "a pull request of a person has no run")

	require.NoError(t, issues_model.SetPullRequestAscRun(t.Context(), pr, 7, "coder-2", "alice"))
	require.NoError(t, issues_model.SetPullRequestAscRun(t.Context(), pr, 7, "coder-2", "alice"), "the same run again is harmless")
	require.Error(t, issues_model.SetPullRequestAscRun(t.Context(), pr, 8, "coder-2", "alice"), "a pull request is not moved to another run")
	require.NoError(t, issues_model.SetPullRequestAscRun(t.Context(), other, 7, "coder-2", "alice"))

	got, err = issues_model.GetPullRequestAscRun(t.Context(), pr.ID)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, uint64(7), got.RunID)
	assert.Equal(t, "coder-2", got.Agent)
	assert.Equal(t, "alice", got.StartedBy)

	list, err := issues_model.ListPullRequestAscRuns(t.Context(), pr.BaseRepoID, 7)
	require.NoError(t, err)
	assert.NotEmpty(t, list)
	for _, r := range list {
		assert.Equal(t, uint64(7), r.RunID)
	}

	for _, bad := range []struct {
		run   uint64
		agent string
	}{{0, "coder-2"}, {7, ""}, {7, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}} {
		assert.Error(t, issues_model.SetPullRequestAscRun(t.Context(), pr, bad.run, bad.agent, ""))
	}
}
