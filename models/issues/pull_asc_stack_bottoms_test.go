// Copyright 2026 The ASC Hub Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package issues_test

import (
	"testing"

	issues_model "forgejo.org/models/issues"
	"forgejo.org/models/unittest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAscStackBottomsAndAdoption(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	bottom := unittest.AssertExistsAndLoadBean(t, &issues_model.PullRequest{ID: 1})
	child := unittest.AssertExistsAndLoadBean(t, &issues_model.PullRequest{ID: 2})
	ctx := t.Context()
	require.NoError(t, issues_model.SetPullRequestAscStack(ctx, child, "kkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkk", 0))
	require.NoError(t, issues_model.SetPullRequestAscStack(ctx, bottom, "llllllllllllllllllllllllllllllll", 0))

	rows, err := issues_model.ListAscStackBottoms(ctx, bottom.BaseRepoID)
	require.NoError(t, err)
	require.Len(t, rows, 2, "both rows have no parent yet")
	assert.Equal(t, child.ID, rows[0].PullID, "in the order they were recorded")

	require.NoError(t, issues_model.SetAscStackParent(ctx, child.ID, bottom.ID))
	rows, err = issues_model.ListAscStackBottoms(ctx, bottom.BaseRepoID)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, bottom.ID, rows[0].PullID, "the linked change is no longer a bottom")
	row, err := issues_model.GetPullRequestAscStack(ctx, child.ID)
	require.NoError(t, err)
	assert.Equal(t, bottom.ID, row.StackParentID)
}
