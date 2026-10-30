// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	"strconv"
	"testing"

	"forgejo.org/models/db"
	issues_model "forgejo.org/models/issues"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unittest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAscQueueScopeSizes(t *testing.T) {
	assert.Equal(t, []int{1}, ascQueueScopeSizes(1))
	assert.Equal(t, []int{1, 2}, ascQueueScopeSizes(2))
	assert.Equal(t, []int{1, 2, 4}, ascQueueScopeSizes(4))
}

// An open stack is listed once, by its top change, with the scopes a land may take and the confirmation page of the change each one
// ends at.
func TestAscQueueStacksOfListsTheStackWithItsScopes(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	ctx := t.Context()
	bottom := unittest.AssertExistsAndLoadBean(t, &issues_model.PullRequest{ID: 2})
	top := unittest.AssertExistsAndLoadBean(t, &issues_model.PullRequest{ID: 5})
	top.BaseBranch = "master"
	_, err := db.GetEngine(ctx).ID(top.ID).Cols("base_branch").Update(top)
	require.NoError(t, err)
	require.NoError(t, issues_model.SetPullRequestAscStack(ctx, bottom, "kkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkk", 0))
	require.NoError(t, issues_model.SetPullRequestAscStack(ctx, top, "llllllllllllllllllllllllllllllll", bottom.ID))
	repo := unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{ID: 1})

	stacks, err := ascQueueStacksOf(ctx, repo, "master", map[int64]bool{})
	require.NoError(t, err)
	require.Len(t, stacks, 1, "the bottom change is part of the stack, not a stack of its own")
	st := stacks[0]
	assert.Equal(t, top.ID, st.Top.ID)
	assert.Equal(t, 2, st.Changes)
	require.Len(t, st.Scopes, 2)
	assert.True(t, st.Scopes[0].Just)
	assert.Contains(t, st.Scopes[0].MergeURL, "/pulls/"+itoaIndex(t, bottom)+"/asc/confirm?op=merge", "just the bottom change ends at the bottom pull request")
	assert.True(t, st.Scopes[1].All)
	assert.Contains(t, st.Scopes[1].QueueURL, "/pulls/"+itoaIndex(t, top)+"/asc/confirm?op=queue")

	other, err := ascQueueStacksOf(ctx, repo, "no-such-branch", map[int64]bool{})
	require.NoError(t, err)
	assert.Empty(t, other, "only stacks that land on the branch")
}

func itoaIndex(t *testing.T, pr *issues_model.PullRequest) string {
	t.Helper()
	require.NoError(t, pr.LoadIssue(t.Context()))
	return strconv.FormatInt(pr.Issue.Index, 10)
}
