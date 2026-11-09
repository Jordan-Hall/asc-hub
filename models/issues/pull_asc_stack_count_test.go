// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package issues_test

import (
	"testing"

	"forgejo.org/models/db"
	issues_model "forgejo.org/models/issues"
	"forgejo.org/models/unittest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCountAscOpenStacks(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	ctx := t.Context()

	got, err := issues_model.CountAscOpenStacks(ctx, nil)
	require.NoError(t, err)
	assert.Empty(t, got)

	// Repository 1 has open pull requests 1, 2 and 5; repository 10 has 3; repository 2 has none.
	base, err := issues_model.CountAscOpenStacks(ctx, []int64{1, 10, 2})
	require.NoError(t, err)
	open1, err := db.GetEngine(ctx).Where("repo_id = ? AND is_pull = ? AND is_closed = ?", 1, true, false).Count(new(issues_model.Issue))
	require.NoError(t, err)
	assert.EqualValues(t, open1, base[1], "with no links every open pull request is a stack of its own")
	assert.Equal(t, 1, base[10])
	_, has := base[2]
	assert.False(t, has, "a repository with no open pull request has no entry")

	link := func(pullID int64, change string, parent int64) {
		pr := unittest.AssertExistsAndLoadBean(t, &issues_model.PullRequest{ID: pullID})
		require.NoError(t, issues_model.SetPullRequestAscStack(ctx, pr, change, parent))
	}
	link(1, "kkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkk", 0)
	link(2, "llllllllllllllllllllllllllllllll", 1)
	got, err = issues_model.CountAscOpenStacks(ctx, []int64{1, 10})
	require.NoError(t, err)
	assert.Equal(t, base[1]-1, got[1], "the parent of an open child is not a tip")
	assert.Equal(t, base[10], got[10])

	// A second child of the same parent is another stack, so the count holds.
	link(5, "mmmmmmmmmmmmmmmmmmmmmmmmmmmmmmmm", 1)
	got, err = issues_model.CountAscOpenStacks(ctx, []int64{1})
	require.NoError(t, err)
	assert.Equal(t, base[1]-1, got[1])

	// A parent in another repository does not end a stack here.
	link(3, "nnnnnnnnnnnnnnnnnnnnnnnnnnnnnnnn", 2)
	got, err = issues_model.CountAscOpenStacks(ctx, []int64{1, 10})
	require.NoError(t, err)
	assert.Equal(t, base[1]-1, got[1])
	assert.Equal(t, base[10], got[10])

	// When the parent closes, its children are tips again.
	_, err = db.GetEngine(ctx).Exec("UPDATE issue SET is_closed = ? WHERE id = ?", true, 2)
	require.NoError(t, err)
	got, err = issues_model.CountAscOpenStacks(ctx, []int64{1})
	require.NoError(t, err)
	assert.Equal(t, base[1]-1, got[1], "one fewer open pull request, and no parent among them")
}
