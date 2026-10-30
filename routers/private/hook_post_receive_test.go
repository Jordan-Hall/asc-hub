// Copyright 2024 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package private

import (
	"testing"

	"forgejo.org/models/db"
	issues_model "forgejo.org/models/issues"
	pull_model "forgejo.org/models/pull"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/private"
	repo_module "forgejo.org/modules/repository"
	"forgejo.org/services/contexttest"

	"forgejo.org/services/ascsync"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHandlePullRequestMerging(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	pr, err := issues_model.GetUnmergedPullRequest(db.DefaultContext, 1, 1, "branch2", "master", issues_model.PullRequestFlowGithub)
	require.NoError(t, err)
	require.NoError(t, pr.LoadBaseRepo(db.DefaultContext))

	user1 := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 1})

	err = pull_model.ScheduleAutoMerge(db.DefaultContext, user1, pr.ID, repo_model.MergeStyleSquash, "squash merge a pr", false)
	require.NoError(t, err)

	autoMerge := unittest.AssertExistsAndLoadBean(t, &pull_model.AutoMerge{PullID: pr.ID})

	ctx, resp := contexttest.MockPrivateContext(t, "/")
	handlePullRequestMerging(ctx, &private.HookOptions{
		PullRequestID: pr.ID,
		UserID:        2,
	}, pr.BaseRepo.OwnerName, pr.BaseRepo.Name, []*repo_module.PushUpdateOptions{
		{NewCommitID: "01234567"},
	})
	assert.Empty(t, resp.Body.String())
	pr, err = issues_model.GetPullRequestByID(db.DefaultContext, pr.ID)
	require.NoError(t, err)
	assert.True(t, pr.HasMerged)
	assert.Equal(t, "01234567", pr.MergedCommitID)

	unittest.AssertNotExistsBean(t, &pull_model.AutoMerge{ID: autoMerge.ID})
}

func TestRefsToSettleLeavesOutRequestsTurnedIntoPullRequests(t *testing.T) {
	refs := []ascsync.PushRef{
		{Name: "refs/for/main/asc-kkkk", New: "1111111111111111111111111111111111111111"},
		{Name: "refs/heads/main", New: "2222222222222222222222222222222222222222"},
		{Name: "refs/pull/3/head", New: "3333333333333333333333333333333333333333"},
	}
	got := refsToSettle(refs)
	assert.Equal(t, []ascsync.PushRef{refs[1], refs[2]}, got, "the repository never holds a refs/for ref")
	assert.Empty(t, refsToSettle(refs[:1]), "nothing to wait for: the wait is skipped")
}
