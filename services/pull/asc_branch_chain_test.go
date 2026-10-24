// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package pull

import (
	"context"
	"errors"
	"testing"

	"forgejo.org/models/db"
	issues_model "forgejo.org/models/issues"
	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Fixture: pull request 2 (head branch2, base master) is the parent of pull request 5 (head pr-to-update,
// base branch2) in repository 1.
func stubBranchChain(t *testing.T, branchless bool, modeErr error) *[]int64 {
	t.Helper()
	var moved []int64
	pb, pt := ascBranchless, ascChangeTarget
	ascBranchless = func(context.Context, *issues_model.PullRequest) (bool, error) { return branchless, modeErr }
	ascChangeTarget = func(_ context.Context, pr *issues_model.PullRequest, _ *user_model.User, target string) error {
		moved = append(moved, pr.ID)
		pr.BaseBranch = target
		return nil
	}
	t.Cleanup(func() { ascBranchless, ascChangeTarget = pb, pt })
	return &moved
}

func mergeParent(t *testing.T, ctx context.Context) *issues_model.PullRequest {
	t.Helper()
	_, err := db.GetEngine(ctx).Exec("UPDATE `pull_request` SET has_merged = ? WHERE id = 2", true)
	require.NoError(t, err)
	return unittest.AssertExistsAndLoadBean(t, &issues_model.PullRequest{ID: 2})
}

func TestAscBranchChainRetargetsOnParentMerge(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	ctx := t.Context()
	moved := stubBranchChain(t, false, nil)
	parent := mergeParent(t, ctx)

	n, err := RetargetAscBranchChain(ctx, parent, nil)
	require.NoError(t, err)
	assert.Equal(t, 1, n)
	assert.Equal(t, []int64{5}, *moved)
}

func TestAscBranchChainNotWhenBranchless(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	ctx := t.Context()
	moved := stubBranchChain(t, true, nil)
	parent := mergeParent(t, ctx)

	n, err := RetargetAscBranchChain(ctx, parent, nil)
	require.NoError(t, err)
	assert.Equal(t, 0, n)
	assert.Empty(t, *moved)
}

func TestAscBranchChainFailsClosedWhenModeUnknown(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	ctx := t.Context()
	moved := stubBranchChain(t, false, errors.New("asc unreachable"))
	parent := mergeParent(t, ctx)

	_, err := RetargetAscBranchChain(ctx, parent, nil)
	require.Error(t, err)
	assert.Empty(t, *moved)
}

func TestAscBranchChainSkipsUnmergedParentForkAndStackedChild(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	ctx := t.Context()
	moved := stubBranchChain(t, false, nil)

	// A parent that has not merged retargets nothing.
	open := unittest.AssertExistsAndLoadBean(t, &issues_model.PullRequest{ID: 2})
	n, err := RetargetAscBranchChain(ctx, open, nil)
	require.NoError(t, err)
	assert.Equal(t, 0, n)

	// A parent whose head branch is in a fork is not a chain parent.
	parent := mergeParent(t, ctx)
	fork := *parent
	fork.HeadRepoID = 99
	children, err := AscBranchChainChildren(ctx, &fork)
	require.NoError(t, err)
	assert.Empty(t, children)

	// A child that has its own asc change follows the stack rules instead.
	child := unittest.AssertExistsAndLoadBean(t, &issues_model.PullRequest{ID: 5})
	require.NoError(t, issues_model.SetPullRequestAscStack(ctx, child, ascTestChange, 0))
	children, err = AscBranchChainChildren(ctx, parent)
	require.NoError(t, err)
	assert.Empty(t, children)
	assert.Empty(t, *moved)
}
