// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	gocontext "context"
	"net/http"
	"net/http/httptest"
	"testing"

	"forgejo.org/models/db"
	issues_model "forgejo.org/models/issues"
	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/git"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/templates"
	"forgejo.org/modules/test"
	"forgejo.org/services/ascagentui"
	"forgejo.org/services/automerge"
	"forgejo.org/services/context"
	"forgejo.org/services/contexttest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAscOpenStateOf(t *testing.T) {
	for _, c := range []struct {
		writing, queued, draft, changes, approved bool
		want                                      string
	}{
		{true, true, true, true, true, ascOpenWriting},
		{false, true, true, true, true, ascOpenQueued},
		{false, false, true, true, true, ascOpenDraft},
		{false, false, false, true, true, ascOpenChanges},
		{false, false, false, false, true, ascOpenApproved},
		{false, false, false, false, false, ascOpenReview},
	} {
		assert.Equal(t, c.want, ascOpenStateOf(c.writing, c.queued, c.draft, c.changes, c.approved))
	}
}

func TestAscOpenStackPlaces(t *testing.T) {
	prs := []*issues_model.PullRequest{
		{ID: 1, HeadBranch: "retry"}, {ID: 2, HeadBranch: "retry-2"}, {ID: 3, HeadBranch: "retry-3"},
		{ID: 4, HeadBranch: "docs"},
		{ID: 5, HeadBranch: "orphan"},
		{ID: 6, HeadBranch: "loop-a"}, {ID: 7, HeadBranch: "loop-b"},
	}
	// 1 <- 2 <- 3 is one stack; 5's parent landed; 6 and 7 point at each other
	parent := map[int64]int64{2: 1, 3: 2, 5: 99, 6: 7, 7: 6}
	name, place, size := ascOpenStackPlaces(prs, parent)
	for id, want := range map[int64]string{1: "retry", 2: "retry", 3: "retry", 4: "docs", 5: "orphan"} {
		assert.Equal(t, want, name[id], id)
	}
	assert.Equal(t, 1, place[1])
	assert.Equal(t, 2, place[2])
	assert.Equal(t, 3, place[3])
	assert.Equal(t, 3, size[1])
	assert.Equal(t, 3, size[3])
	assert.Equal(t, 1, size[4])
	assert.Equal(t, 1, size[5], "a parent that is not open is not part of the stack")
	assert.LessOrEqual(t, place[6], len(prs), "a loop ends")
}

func TestAscOpenReviews(t *testing.T) {
	// the names follow the site's choice of full names
	defer test.MockVariableValue(&setting.UI.DefaultShowFullName, true)()
	priya := &user_model.User{Name: "priya", FullName: "Priya Raman"}
	sam := &user_model.User{Name: "sam"}
	changes, approved, asked, approvers, rejecters := ascOpenReviews(issues_model.ReviewList{
		{Type: issues_model.ReviewTypeApprove, Reviewer: sam},
		{Type: issues_model.ReviewTypeRequest, Reviewer: priya},
		{Type: issues_model.ReviewTypeComment, Reviewer: sam},
		{Type: issues_model.ReviewTypeApprove},
	})
	assert.False(t, changes)
	assert.False(t, approved, "a review is still asked for")
	assert.Equal(t, []string{"Priya Raman"}, asked)
	assert.Equal(t, []string{"sam"}, approvers)
	assert.Empty(t, rejecters)
	assert.Equal(t, "Priya Raman", ascOpenPeople(ascOpenReview, asked, approvers, rejecters))
	assert.Equal(t, "sam", ascOpenPeople(ascOpenApproved, asked, approvers, rejecters))
	assert.Empty(t, ascOpenPeople(ascOpenQueued, asked, approvers, rejecters))

	changes, approved, _, _, rejecters = ascOpenReviews(issues_model.ReviewList{{Type: issues_model.ReviewTypeReject, Reviewer: priya}, {Type: issues_model.ReviewTypeApprove, Reviewer: sam}})
	assert.True(t, changes)
	assert.True(t, approved)
	assert.Equal(t, "Priya Raman", ascOpenPeople(ascOpenChanges, nil, nil, rejecters))
}

func TestAscLandedBrackets(t *testing.T) {
	order := []string{"c1", "c2", "c3", "c4", "c5", "c6", "c7"}
	landed := []*issues_model.LandedAscStackPull{
		// 10 <- 11 <- 12 landed as c4, c3, c2 (newest first)
		{PullID: 10, MergedCommitID: "c4"}, {PullID: 11, MergedCommitID: "c3", StackParentID: 10}, {PullID: 12, MergedCommitID: "c2", StackParentID: 11},
		// a change alone
		{PullID: 20, MergedCommitID: "c5"},
		// 30 <- 31 are not next to each other
		{PullID: 30, MergedCommitID: "c7"}, {PullID: 31, MergedCommitID: "c1", StackParentID: 30},
		// not shown
		{PullID: 40, MergedCommitID: "gone", StackParentID: 20},
	}
	assert.Equal(t, map[string]string{"c2": ascBracketStart, "c3": ascBracketMid, "c4": ascBracketEnd}, ascLandedBrackets(order, landed))
	assert.Empty(t, ascLandedBrackets(order, nil))
}

func TestPrepareAscOpenChanges(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	defer test.MockVariableValue(&setting.Asc.Enabled, true)()
	defer test.MockVariableValue(&ascOpenRunningRuns, func(gocontext.Context, string) (map[uint64]*ascv1.AgentRun, error) {
		return nil, nil
	})()

	page := func(t *testing.T, user int64, backend, query string) (*context.Context, *httptest.ResponseRecorder) {
		t.Helper()
		ctx, resp := contexttest.MockContext(t, "/user2/repo1/commits/branch/master"+query)
		ctx.Render = templates.HTMLRenderer()
		ctx.Req.Header.Set("Accept", "text/html")
		contexttest.LoadUser(t, ctx, user)
		ctx.IsSigned = true
		contexttest.LoadRepo(t, ctx, 1)
		contexttest.LoadGitRepo(t, ctx)
		t.Cleanup(func() { ctx.Repo.GitRepo.Close() })
		ctx.Repo.Repository.StorageBackend = backend
		ctx.Repo.IsViewBranch, ctx.Repo.BranchName, ctx.Repo.RefName = true, "master", "master"
		prepareAscOpenChanges(ctx)
		return ctx, resp
	}
	only := func(t *testing.T, ctx *context.Context) *AscOpenChange {
		t.Helper()
		got, ok := ctx.Data["AscOpenChanges"].([]*AscOpenChange)
		require.True(t, ok)
		require.Len(t, got, 1)
		return got[0]
	}

	t.Run("the open change onto the branch", func(t *testing.T) {
		// fixture pull 2 (issue 3): user1's open branch2 onto master, not queued; a reviewer asked for changes
		c := only(t, func() *context.Context { ctx, _ := page(t, 2, "asc", ""); return ctx }())
		assert.Equal(t, "issue3", c.Title)
		assert.Equal(t, "/user2/repo1/pulls/3", c.Link)
		assert.Equal(t, ascOpenChanges, c.State)
		assert.NotEmpty(t, c.People, "the status line names who asked for changes")
		assert.True(t, c.HasStat)
		assert.Equal(t, 10, c.Additions)
		assert.Equal(t, 1, c.Position)
		assert.Equal(t, 1, c.Size)
		assert.Equal(t, "branch2", c.Stack)
		require.NotNil(t, c.Poster)
		assert.Equal(t, int64(1), c.Poster.ID)
		assert.Len(t, c.Change, ascChangeIDShort, "the head commit names a change asc has not named")
		assert.Zero(t, c.Run)
	})
	t.Run("a change in the merge queue says how many are ahead", func(t *testing.T) {
		require.NoError(t, db.Insert(t.Context(), &automerge.AscStackQueueEntry{RepoID: 1, Branch: "master", PullID: 9998, AscChange: "k", State: automerge.AscQueueLanding}))
		require.NoError(t, db.Insert(t.Context(), &automerge.AscStackQueueEntry{RepoID: 1, Branch: "master", PullID: 2, AscChange: "k", State: automerge.AscQueueQueued}))
		ctx, resp := page(t, 2, "asc", "")
		c := only(t, ctx)
		assert.Equal(t, ascOpenQueued, c.State)
		assert.Equal(t, 2, c.QueuePlace)
		assert.Equal(t, 1, c.QueueAhead)

		ctx.HTML(http.StatusOK, "repo/commits_asc_open")
		body := resp.Body.String()
		assert.Contains(t, body, `data-testid="asc-open-changes"`)
		assert.Contains(t, body, `data-state="queued"`)
		assert.Contains(t, body, `rp-open-node is-queued`)
		assert.Contains(t, body, `asc_rp.open_chip_queued`)
		assert.Contains(t, body, `href="/user2/repo1/pulls/3"`)
		assert.Contains(t, body, `asc_rp.open_status_queue_1`)
	})
	t.Run("an agent run that is still going makes it writing, for someone who may read runs", func(t *testing.T) {
		require.NoError(t, issues_model.SetPullRequestAscRun(t.Context(), &issues_model.PullRequest{ID: 2, BaseRepoID: 1}, 214, "coder", "user2"))
		defer test.MockVariableValue(&ascOpenRunningRuns, func(gocontext.Context, string) (map[uint64]*ascv1.AgentRun, error) {
			return map[uint64]*ascv1.AgentRun{214: {Id: 214}}, nil
		})()
		ctx, _ := page(t, 2, "asc", "")
		assert.Equal(t, ascOpenQueued, only(t, ctx).State, "the agent features are off: the run is not named")

		require.NoError(t, ascagentui.SetFeatures(t.Context(), ctx.Doer, true))
		ctx, resp := page(t, 2, "asc", "")
		c := only(t, ctx)
		assert.Equal(t, ascOpenWriting, c.State)
		assert.Equal(t, uint64(214), c.Run)
		ctx.HTML(http.StatusOK, "repo/commits_asc_open")
		assert.Contains(t, resp.Body.String(), `rp-open-node is-writing`)
		assert.Contains(t, resp.Body.String(), `asc_rp.open_chip_writing`)
	})
	t.Run("a later page and a git repository get none", func(t *testing.T) {
		ctx, _ := page(t, 2, "asc", "?page=2")
		assert.Nil(t, ctx.Data["AscOpenChanges"])
		ctx, _ = page(t, 2, "git", "")
		assert.Nil(t, ctx.Data["AscOpenChanges"])
	})
}

func TestPrepareAscLandedBrackets(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	defer test.MockVariableValue(&setting.Asc.Enabled, true)()
	// fixture pull 1 is merged; make it and pull 2 one landed stack on two commits
	shas := []string{"65f1bf27bc3bf70f64657658635e66094edbcb4d", "4a357436d925b5c974181ff12a994538ddc5a269"}
	_, err := db.GetEngine(t.Context()).Exec("UPDATE pull_request SET has_merged = ?, merged_commit_id = ? WHERE id = ?", true, shas[0], 1)
	require.NoError(t, err)
	_, err = db.GetEngine(t.Context()).Exec("UPDATE pull_request SET has_merged = ?, merged_commit_id = ? WHERE id = ?", true, shas[1], 2)
	require.NoError(t, err)
	require.NoError(t, db.Insert(t.Context(), &issues_model.PullRequestAscStack{PullID: 1, RepoID: 1, AscChange: "kkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkk"}))
	require.NoError(t, db.Insert(t.Context(), &issues_model.PullRequestAscStack{PullID: 2, RepoID: 1, AscChange: "llllllllllllllllllllllllllllllll", StackParentID: 1}))

	commits := []*git.Commit{{ID: git.MustIDFromString(shas[1])}, {ID: git.MustIDFromString(shas[0])}}
	for _, backend := range []string{"asc", "git"} {
		ctx, _ := contexttest.MockContext(t, "/user2/repo1/commits/branch/master")
		contexttest.LoadUser(t, ctx, 2)
		contexttest.LoadRepo(t, ctx, 1)
		ctx.Repo.Repository.StorageBackend = backend
		prepareAscLandedBrackets(ctx, commits)
		if backend == "git" {
			assert.Nil(t, ctx.Data["AscLandedBrackets"])
			continue
		}
		assert.Equal(t, map[string]string{shas[1]: ascBracketStart, shas[0]: ascBracketEnd}, ctx.Data["AscLandedBrackets"])
	}
}
