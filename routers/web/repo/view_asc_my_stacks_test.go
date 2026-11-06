// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	"fmt"
	"testing"

	issues_model "forgejo.org/models/issues"
	"forgejo.org/models/unittest"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/test"
	"forgejo.org/services/context"
	"forgejo.org/services/contexttest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAscMyStacks(t *testing.T) {
	link := func(branch string) string { return "/o/r/_edit/" + branch + "/a.txt" }
	prs := []*issues_model.PullRequest{
		{HeadRepoID: 1, HeadBranch: "retry", Issue: &issues_model.Issue{PosterID: 7, Title: "Retry with backoff"}},
		{HeadRepoID: 1, HeadBranch: "other", Issue: &issues_model.Issue{PosterID: 8, Title: "Someone else's"}},
		{HeadRepoID: 2, HeadBranch: "fork", Issue: &issues_model.Issue{PosterID: 7, Title: "From a fork"}},
		{HeadRepoID: 1, HeadBranch: "", Issue: &issues_model.Issue{PosterID: 7, Title: "No head"}},
		{HeadRepoID: 1, HeadBranch: "noissue"},
	}
	got := ascMyStacks(prs, 1, 7, link)
	require.Len(t, got, 1, "only the person's own stacks from the repository itself")
	assert.Equal(t, AscMyStack{Title: "Retry with backoff", EditLink: "/o/r/_edit/retry/a.txt"}, got[0])
	assert.Empty(t, ascMyStacks(prs, 1, 9, link))
}

func TestAscMyStacksIsCapped(t *testing.T) {
	prs := make([]*issues_model.PullRequest, ascMyStacksMax+5)
	for i := range prs {
		prs[i] = &issues_model.PullRequest{HeadRepoID: 1, HeadBranch: fmt.Sprintf("s%d", i), Issue: &issues_model.Issue{PosterID: 7, Title: "t"}}
	}
	got := ascMyStacks(prs, 1, 7, func(b string) string { return b })
	assert.Len(t, got, ascMyStacksMax)
	assert.Equal(t, "s0", got[0].EditLink, "the order of the list is kept")
}

func TestPrepareAscMyStacks(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	defer test.MockVariableValue(&setting.Asc.Enabled, true)()

	page := func(t *testing.T, user int64, backend string) *context.Context {
		t.Helper()
		ctx, _ := contexttest.MockContext(t, "/user2/repo1/src/branch/master/README.md")
		contexttest.LoadUser(t, ctx, user)
		contexttest.LoadRepo(t, ctx, 1)
		ctx.Repo.Repository.StorageBackend = backend
		ctx.Repo.IsViewBranch, ctx.Repo.BranchName, ctx.Repo.TreePath = true, "master", "README.md"
		prepareAscMyStacks(ctx)
		return ctx
	}

	t.Run("the person's open stack onto the shown branch", func(t *testing.T) {
		// fixture pull 2: user1's open branch2 onto master (pull 1 is merged)
		ctx := page(t, 1, "asc")
		assert.Equal(t, []AscMyStack{{Title: "issue3", EditLink: "/user2/repo1/_edit/branch2/README.md"}}, ctx.Data["AscMyStacks"])
	})
	t.Run("someone with no open stack gets none", func(t *testing.T) {
		assert.Nil(t, page(t, 2, "asc").Data["AscMyStacks"])
	})
	t.Run("someone who cannot write the code gets none", func(t *testing.T) {
		assert.Nil(t, page(t, 4, "asc").Data["AscMyStacks"])
	})
	t.Run("a git repository has no stacks bar", func(t *testing.T) {
		assert.Nil(t, page(t, 1, "git").Data["AscMyStacks"])
	})
}
