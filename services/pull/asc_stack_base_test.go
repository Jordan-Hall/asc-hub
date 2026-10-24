// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package pull

import (
	"context"
	"errors"
	"strings"
	"testing"

	"forgejo.org/models/db"
	issues_model "forgejo.org/models/issues"
	"forgejo.org/models/unittest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	ascTestChange = "kkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkk"
	ascTestParent = "llllllllllllllllllllllllllllllll"
	ascTestFirst  = "1111111111111111111111111111111111111111"
	ascTestTrunk  = "2222222222222222222222222222222222222222"
)

func stubFirstParent(t *testing.T, id string, err error) {
	t.Helper()
	prev := ascHeadFirstParent
	ascHeadFirstParent = func(context.Context, *issues_model.PullRequest) (string, error) { return id, err }
	t.Cleanup(func() { ascHeadFirstParent = prev })
}

func TestAscStackMergeBase(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	ctx := t.Context()
	stubFirstParent(t, ascTestFirst, nil)

	child := unittest.AssertExistsAndLoadBean(t, &issues_model.PullRequest{ID: 5})
	openParent := unittest.AssertExistsAndLoadBean(t, &issues_model.PullRequest{ID: 2})
	mergedParent := unittest.AssertExistsAndLoadBean(t, &issues_model.PullRequest{ID: 1})
	require.False(t, openParent.HasMerged)
	require.True(t, mergedParent.HasMerged)
	child.HeadCommitID = "3333333333333333333333333333333333333333"

	// Not stacked: the ordinary merge base stays.
	base, pinned, err := AscStackMergeBase(ctx, child)
	require.NoError(t, err)
	assert.False(t, pinned)
	assert.Empty(t, base)

	// The bottom of a stack has no parent PR: the ordinary rule.
	require.NoError(t, issues_model.SetPullRequestAscStack(ctx, child, ascTestChange, 0))
	_, pinned, err = AscStackMergeBase(ctx, child)
	require.NoError(t, err)
	assert.False(t, pinned)

	// A parent that is not landed: merge_base is the head's first parent.
	require.NoError(t, issues_model.SetPullRequestAscStack(ctx, child, ascTestChange, openParent.ID))
	base, pinned, err = AscStackMergeBase(ctx, child)
	require.NoError(t, err)
	assert.True(t, pinned)
	assert.Equal(t, ascTestFirst, base)

	// A landed parent: the merge base with the trunk applies again.
	require.NoError(t, issues_model.SetPullRequestAscStack(ctx, child, ascTestChange, mergedParent.ID))
	_, pinned, err = AscStackMergeBase(ctx, child)
	require.NoError(t, err)
	assert.False(t, pinned)

	// Git failure is an error, never a silent fallback to the trunk's merge base.
	require.NoError(t, issues_model.SetPullRequestAscStack(ctx, child, ascTestChange, openParent.ID))
	stubFirstParent(t, "", errors.New("boom"))
	_, _, err = AscStackMergeBase(ctx, child)
	require.Error(t, err)
}

func TestAscStackPushDoesNotChangeMergeBase(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	ctx := t.Context()
	stubFirstParent(t, ascTestFirst, nil)

	child := unittest.AssertExistsAndLoadBean(t, &issues_model.PullRequest{ID: 5})
	require.NoError(t, issues_model.SetPullRequestAscStack(ctx, child, ascTestChange, 2))
	child.HeadCommitID = "3333333333333333333333333333333333333333"

	// testPatch has just computed the merge base from the (moved) base branch; the patch replaces it.
	child.MergeBase = ascTestTrunk
	require.NoError(t, applyAscStackBase(ctx, child))
	assert.Equal(t, ascTestFirst, child.MergeBase)

	// The same after the stored value is the old trunk merge base, and after RecomputeAscStackBase.
	child.MergeBase = ascTestTrunk
	require.NoError(t, RecomputeAscStackBase(ctx, child))
	assert.Equal(t, ascTestFirst, child.MergeBase)
	stored := unittest.AssertExistsAndLoadBean(t, &issues_model.PullRequest{ID: 5})
	assert.Equal(t, ascTestFirst, stored.MergeBase)

	// A pull request that is not stacked keeps what testPatch computed.
	other := unittest.AssertExistsAndLoadBean(t, &issues_model.PullRequest{ID: 2})
	other.MergeBase = ascTestTrunk
	require.NoError(t, applyAscStackBase(ctx, other))
	assert.Equal(t, ascTestTrunk, other.MergeBase)
}

func TestAscStackFindByChange(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	ctx := t.Context()

	_, err := issues_model.GetOpenPullRequestByAscChange(ctx, 1, ascTestChange)
	require.Error(t, err)
	assert.True(t, issues_model.IsErrPullRequestNotExist(err))

	child := unittest.AssertExistsAndLoadBean(t, &issues_model.PullRequest{ID: 5})
	require.NoError(t, issues_model.SetPullRequestAscStack(ctx, child, ascTestChange, 2))
	got, err := issues_model.GetOpenPullRequestByAscChange(ctx, 1, ascTestChange)
	require.NoError(t, err)
	assert.Equal(t, int64(5), got.ID)

	// Another repository does not see it.
	_, err = issues_model.GetOpenPullRequestByAscChange(ctx, 3, ascTestChange)
	assert.True(t, issues_model.IsErrPullRequestNotExist(err))

	// A PR has one change.
	require.Error(t, issues_model.SetPullRequestAscStack(ctx, child, ascTestParent, 0))
	// It cannot be its own parent, and a bad change id is refused.
	require.Error(t, issues_model.SetPullRequestAscStack(ctx, child, ascTestChange, child.ID))
	require.Error(t, issues_model.SetPullRequestAscStack(ctx, child, strings.Repeat("a", 32), 0))

	// A merged PR is no longer found: a new submit opens a new one.
	_, err = db.GetEngine(ctx).Exec("UPDATE `pull_request` SET has_merged = ? WHERE id = ?", true, child.ID)
	require.NoError(t, err)
	_, err = issues_model.GetOpenPullRequestByAscChange(ctx, 1, ascTestChange)
	assert.True(t, issues_model.IsErrPullRequestNotExist(err))
}
