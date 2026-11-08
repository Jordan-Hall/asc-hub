// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package pull

import (
	"context"
	"errors"
	"testing"

	"forgejo.org/models/db"
	issues_model "forgejo.org/models/issues"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/git"
	"forgejo.org/modules/repository"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/test"
	"forgejo.org/services/ascbridge"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type autoRestackCall struct{ doerID, pullID int64 }

// setupAutoRestack makes pull request 2 the bottom of a stack of repository 1 (5 is on top of it), stubs the
// restack and returns the calls it receives.
func setupAutoRestack(t *testing.T, rules ascbridge.StackRules, rulesErr error) (*issues_model.PullRequest, *[]autoRestackCall) {
	t.Helper()
	bottom, _, _ := ascMergeFixture(t)
	t.Cleanup(UseAscStackBacked(func(*repo_model.Repository) (bool, error) { return true, nil }))
	t.Cleanup(ascbridge.UseStackRules(func(context.Context, string) (ascbridge.StackRules, error) { return rules, rulesErr }))
	calls := &[]autoRestackCall{}
	prev := ascAutoRestackRun
	ascAutoRestackRun = func(_ context.Context, doer *user_model.User, pr *issues_model.PullRequest) (*AscRestackStackResult, error) {
		*calls = append(*calls, autoRestackCall{doer.ID, pr.ID})
		return &AscRestackStackResult{Applied: true}, nil
	}
	t.Cleanup(func() { ascAutoRestackRun = prev })
	return bottom, calls
}

func TestAscRestackOnTrunkMoveRestacksAsThePoster(t *testing.T) {
	bottom, calls := setupAutoRestack(t, ascbridge.StackRules{RestackOnMainMove: true}, nil)
	require.NoError(t, AscRestackOnTrunkMove(t.Context(), bottom.BaseRepoID))
	// Only the bottom of the stack is restacked (the restack carries the pull requests above it), as its poster.
	assert.Equal(t, []autoRestackCall{{doerID: 1, pullID: bottom.ID}}, *calls)
}

func TestAscRestackOnTrunkMoveNeedsTheSwitch(t *testing.T) {
	bottom, calls := setupAutoRestack(t, ascbridge.StackRules{CollaboratorsMayPush: true}, nil)
	require.NoError(t, AscRestackOnTrunkMove(t.Context(), bottom.BaseRepoID))
	assert.Empty(t, *calls)
}

func TestAscRestackOnTrunkMoveReportsAnUnreadablePolicy(t *testing.T) {
	bottom, calls := setupAutoRestack(t, ascbridge.StackRules{}, errors.New("asc unavailable"))
	require.Error(t, AscRestackOnTrunkMove(t.Context(), bottom.BaseRepoID))
	assert.Empty(t, *calls)
}

func TestAscRestackOnTrunkMoveSkipsStacksOffTrunk(t *testing.T) {
	bottom, calls := setupAutoRestack(t, ascbridge.StackRules{RestackOnMainMove: true}, nil)
	bottom.BaseBranch = "release"
	_, err := db.GetEngine(t.Context()).ID(bottom.ID).Cols("base_branch").Update(bottom)
	require.NoError(t, err)
	require.NoError(t, AscRestackOnTrunkMove(t.Context(), bottom.BaseRepoID))
	assert.Empty(t, *calls)
}

func TestAscRestackOnTrunkMoveSkipsClosedStacks(t *testing.T) {
	bottom, calls := setupAutoRestack(t, ascbridge.StackRules{RestackOnMainMove: true}, nil)
	_, err := db.GetEngine(t.Context()).ID(bottom.IssueID).Cols("is_closed").Update(&issues_model.Issue{IsClosed: true})
	require.NoError(t, err)
	require.NoError(t, AscRestackOnTrunkMove(t.Context(), bottom.BaseRepoID))
	assert.Empty(t, *calls)
}

func TestAscRestackOnTrunkMoveLeavesAFailedStack(t *testing.T) {
	bottom, calls := setupAutoRestack(t, ascbridge.StackRules{RestackOnMainMove: true}, nil)
	ascAutoRestackRun = func(_ context.Context, doer *user_model.User, pr *issues_model.PullRequest) (*AscRestackStackResult, error) {
		*calls = append(*calls, autoRestackCall{doer.ID, pr.ID})
		return nil, ErrAscRestackNotAllowed
	}
	// A stack the poster may not rewrite, or one that conflicts, is left for a person; the job does not fail.
	require.NoError(t, AscRestackOnTrunkMove(t.Context(), bottom.BaseRepoID))
	assert.Len(t, *calls, 1)
}

func TestAscRestackOnTrunkMoveIgnoresOtherRepositories(t *testing.T) {
	bottom, calls := setupAutoRestack(t, ascbridge.StackRules{RestackOnMainMove: true}, nil)
	t.Cleanup(UseAscStackBacked(func(*repo_model.Repository) (bool, error) { return false, nil }))
	require.NoError(t, AscRestackOnTrunkMove(t.Context(), bottom.BaseRepoID))
	assert.Empty(t, *calls)
}

func TestAscTrunkMoved(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	defer test.MockVariableValue(&setting.Asc.Enabled, true)()
	t.Cleanup(UseAscStackBacked(func(*repo_model.Repository) (bool, error) { return true, nil }))
	repo := unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{ID: 1})
	push := func(ref, newID string) *repository.PushUpdateOptions {
		return &repository.PushUpdateOptions{RefFullName: git.RefName(ref), OldCommitID: gitHexN('a'), NewCommitID: newID}
	}
	assert.True(t, ascTrunkMoved(repo, push("refs/heads/"+repo.DefaultBranch, gitHexN('b'))))
	assert.False(t, ascTrunkMoved(repo, push("refs/heads/feature", gitHexN('b'))), "another branch")
	assert.False(t, ascTrunkMoved(repo, push("refs/tags/"+repo.DefaultBranch, gitHexN('b'))), "a tag of the same name")
	assert.False(t, ascTrunkMoved(repo, push("refs/heads/"+repo.DefaultBranch, git.Sha1ObjectFormat.EmptyObjectID().String())), "a delete")
	assert.False(t, ascTrunkMoved(nil, push("refs/heads/master", gitHexN('b'))))

	t.Cleanup(UseAscStackBacked(func(*repo_model.Repository) (bool, error) { return false, nil }))
	assert.False(t, ascTrunkMoved(repo, push("refs/heads/"+repo.DefaultBranch, gitHexN('b'))), "not an asc repository")
	defer test.MockVariableValue(&setting.Asc.Enabled, false)()
	assert.False(t, ascTrunkMoved(repo, push("refs/heads/"+repo.DefaultBranch, gitHexN('b'))))
}
