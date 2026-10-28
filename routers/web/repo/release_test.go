// Copyright 2017 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package repo

import (
	"testing"

	"forgejo.org/models/db"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unit"
	"forgejo.org/models/unittest"
	"forgejo.org/modules/optional"
	"forgejo.org/modules/web"
	"forgejo.org/services/contexttest"
	"forgejo.org/services/forms"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewReleasePost(t *testing.T) {
	for _, testCase := range []struct {
		RepoID  int64
		UserID  int64
		TagName string
		Form    forms.NewReleaseForm
	}{
		{
			RepoID:  1,
			UserID:  2,
			TagName: "v1.1", // pre-existing tag
			Form: forms.NewReleaseForm{
				TagName: "newtag",
				Target:  "master",
				Title:   "title",
				Content: "content",
			},
		},
		{
			RepoID:  1,
			UserID:  2,
			TagName: "newtag",
			Form: forms.NewReleaseForm{
				TagName: "newtag",
				Target:  "master",
				Title:   "title",
				Content: "content",
			},
		},
	} {
		unittest.PrepareTestEnv(t)

		ctx, _ := contexttest.MockContext(t, "user2/repo1/releases/new")
		contexttest.LoadUser(t, ctx, 2)
		contexttest.LoadRepo(t, ctx, 1)
		contexttest.LoadGitRepo(t, ctx)
		web.SetForm(ctx, &testCase.Form)
		NewReleasePost(ctx)
		unittest.AssertExistsAndLoadBean(t, &repo_model.Release{
			RepoID:      1,
			PublisherID: 2,
			TagName:     testCase.Form.TagName,
			Target:      testCase.Form.Target,
			Title:       testCase.Form.Title,
			Note:        testCase.Form.Content,
		}, unittest.Cond("is_draft=?", len(testCase.Form.Draft) > 0))
		ctx.Repo.GitRepo.Close()
	}
}

func TestCalReleaseNumCommitsBehind(t *testing.T) {
	unittest.PrepareTestEnv(t)
	ctx, _ := contexttest.MockContext(t, "user2/repo-release/releases")
	contexttest.LoadUser(t, ctx, 2)
	contexttest.LoadRepo(t, ctx, 57)
	contexttest.LoadGitRepo(t, ctx)
	t.Cleanup(func() { ctx.Repo.GitRepo.Close() })

	releases, err := db.Find[repo_model.Release](ctx, repo_model.FindReleasesOptions{
		IncludeDrafts: ctx.Repo.CanWrite(unit.TypeReleases),
		RepoID:        ctx.Repo.Repository.ID,
	})
	require.NoError(t, err)

	countCache := make(map[string]int64)
	for _, release := range releases {
		err := calReleaseNumCommitsBehind(ctx.Repo, release, countCache)
		require.NoError(t, err)
	}

	type computedFields struct {
		NumCommitsBehind int64
		TargetBehind     string
	}
	expectedComputation := map[string]computedFields{
		"v1.0": {
			NumCommitsBehind: 3,
			TargetBehind:     "main",
		},
		"v1.1": {
			NumCommitsBehind: 1,
			TargetBehind:     "main",
		},
		"v2.0": {
			NumCommitsBehind: 0,
			TargetBehind:     "main",
		},
		"non-existing-target-branch": {
			NumCommitsBehind: 1,
			TargetBehind:     "main",
		},
		"empty-target-branch": {
			NumCommitsBehind: 1,
			TargetBehind:     "main",
		},
	}
	for _, r := range releases {
		actual := computedFields{
			NumCommitsBehind: r.NumCommitsBehind,
			TargetBehind:     r.TargetBehind,
		}
		assert.Equal(t, expectedComputation[r.TagName], actual, "wrong computed fields for %s: %#v", r.TagName, r)
	}
}

func Test_getReleaseInfos(t *testing.T) {
	testCases := []struct {
		name              string
		listOptions       db.ListOptions
		expectedRepoCount int
		expectedCount     int64
	}{
		{
			name:              "page 1 with page size 1",
			listOptions:       db.ListOptions{Page: 1, PageSize: 1},
			expectedRepoCount: 1,
			expectedCount:     3,
		},
		{
			name:              "page 1 with page size 10",
			listOptions:       db.ListOptions{Page: 1, PageSize: 10},
			expectedRepoCount: 3,
			expectedCount:     3,
		},
		{
			name:              "list all",
			listOptions:       db.ListOptions{ListAll: true},
			expectedRepoCount: 3,
			expectedCount:     3,
		},
	}

	unittest.PrepareTestEnv(t)
	ctx, _ := contexttest.MockContext(t, "user1/repo-release/releases")
	contexttest.LoadRepo(t, ctx, 1)
	contexttest.LoadGitRepo(t, ctx)
	assert.NoError(t, db.Insert(ctx, &repo_model.Release{RepoID: ctx.Repo.Repository.ID, Title: "myrel 1", TagName: "myrel_v1.0"}))
	assert.NoError(t, db.Insert(ctx, &repo_model.Release{RepoID: ctx.Repo.Repository.ID, Title: "myrel 2", TagName: "myrel_v2.0"}))
	assert.NoError(t, db.Insert(ctx, &repo_model.Release{RepoID: ctx.Repo.Repository.ID, Title: "myrel 3", TagName: ""}))

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			repos, count, err := getReleaseInfos(ctx, &repo_model.FindReleasesOptions{
				RepoID:      ctx.Repo.Repository.ID,
				Keyword:     "myrel",
				ListOptions: tc.listOptions,
			})
			require.NoError(t, err)
			assert.Len(t, repos, tc.expectedRepoCount)
			assert.Equal(t, tc.expectedCount, count)
		})
	}
}

func TestReleaseChannel(t *testing.T) {
	assert.Equal(t, "stable", releaseChannel("stable", false))
	assert.Equal(t, "pre", releaseChannel("pre", false))
	assert.Equal(t, "draft", releaseChannel("draft", true))
	assert.Equal(t, "all", releaseChannel("draft", false), "a reader cannot see drafts")
	assert.Equal(t, "all", releaseChannel("nonsense", true))
	assert.Equal(t, "all", releaseChannel("", true))
}

func TestApplyReleaseChannel(t *testing.T) {
	opts := &repo_model.FindReleasesOptions{}
	applyReleaseChannel(opts, "stable")
	assert.Equal(t, optional.Some(false), opts.IsPreRelease)
	assert.Equal(t, optional.Some(false), opts.IsDraft)

	opts = &repo_model.FindReleasesOptions{}
	applyReleaseChannel(opts, "pre")
	assert.Equal(t, optional.Some(true), opts.IsPreRelease)
	assert.Equal(t, optional.Some(false), opts.IsDraft)

	opts = &repo_model.FindReleasesOptions{}
	applyReleaseChannel(opts, "draft")
	assert.Equal(t, optional.Some(true), opts.IsDraft)
	assert.Equal(t, optional.None[bool](), opts.IsPreRelease)

	opts = &repo_model.FindReleasesOptions{}
	applyReleaseChannel(opts, "all")
	assert.Equal(t, optional.None[bool](), opts.IsDraft)
}

func TestLatestStable(t *testing.T) {
	mk := func(tag string, draft, pre, isTag bool) *ReleaseInfo {
		return &ReleaseInfo{Release: &repo_model.Release{TagName: tag, IsDraft: draft, IsPrerelease: pre, IsTag: isTag}}
	}
	list := []*ReleaseInfo{mk("v3", true, false, false), mk("v2rc", false, true, false), mk("bare", false, false, true), mk("v2", false, false, false), mk("v1", false, false, false)}
	assert.Equal(t, "v2", latestStable(list).Release.TagName)
	assert.Nil(t, latestStable(list[:3]))
	assert.Nil(t, latestStable(nil))
}
