// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package integration

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"

	"forgejo.org/models/db"
	issues_model "forgejo.org/models/issues"
	access_model "forgejo.org/models/perm/access"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/git"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/test"
	"forgejo.org/services/ascbridge"
	"forgejo.org/services/automerge"
	pull_service "forgejo.org/services/pull"
	"forgejo.org/tests"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The merge queue, merge now, restack and revert of a stack through the full router (docs/asc/STACK-OPERATIONS.md).
// asc itself is replaced: the lander, the Stacks client, the heads and the test that the repository is in asc are
// fakes, so these tests prove the routes, the permission checks, the dry run pages and the queue, not asc.

const (
	ascQueueBottomChange = "kkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkk"
	ascQueueTopChange    = "llllllllllllllllllllllllllllllll"
)

type ascQueueLander struct {
	mu    sync.Mutex
	lands []int64
}

func (l *ascQueueLander) Land(_ context.Context, _ *user_model.User, _ access_model.Permission, pr *issues_model.PullRequest, _ pull_service.AscMergeOptions) (*pull_service.AscStackMergeResult, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.lands = append(l.lands, pr.ID)
	return &pull_service.AscStackMergeResult{Merged: []*issues_model.PullRequest{pr}}, nil
}

func (l *ascQueueLander) Restack(context.Context, *user_model.User, *issues_model.PullRequest) error {
	return nil
}

func (l *ascQueueLander) landed() []int64 {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]int64(nil), l.lands...)
}

type ascQueueStackOps struct {
	mu      sync.Mutex
	restack *ascbridge.RestackStackResult
	revert  *ascbridge.RevertLandedResult
	calls   []string
}

func (o *ascQueueStackOps) RestackStack(_ context.Context, req ascbridge.RestackStackRequest) (*ascbridge.RestackStackResult, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.calls = append(o.calls, fmt.Sprintf("restack dry=%v", req.DryRun))
	return o.restack, nil
}

func (o *ascQueueStackOps) RevertLanded(_ context.Context, req ascbridge.RevertLandedRequest) (*ascbridge.RevertLandedResult, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.calls = append(o.calls, fmt.Sprintf("revert dry=%v squash=%v", req.DryRun, req.Squash))
	return o.revert, nil
}

type ascQueueEnv struct {
	bottom, top *issues_model.PullRequest
	lander      *ascQueueLander
	ops         *ascQueueStackOps
}

// newAscQueueEnv links pull requests 2 and 5 of user2/repo1 as a stack on master and fakes asc.
func newAscQueueEnv(t *testing.T) *ascQueueEnv {
	t.Helper()
	t.Cleanup(test.MockVariableValue(&setting.Asc.Enabled, true))
	ctx := t.Context()
	e := &ascQueueEnv{lander: &ascQueueLander{}, ops: &ascQueueStackOps{}}
	e.bottom = unittest.AssertExistsAndLoadBean(t, &issues_model.PullRequest{ID: 2})
	e.top = unittest.AssertExistsAndLoadBean(t, &issues_model.PullRequest{ID: 5})
	_, err := db.GetEngine(ctx).Exec("UPDATE `pull_request` SET base_branch = ? WHERE id = ?", "master", e.top.ID)
	require.NoError(t, err)
	e.top.BaseBranch = "master"
	require.NoError(t, issues_model.SetPullRequestAscStack(ctx, e.bottom, ascQueueBottomChange, 0))
	require.NoError(t, issues_model.SetPullRequestAscStack(ctx, e.top, ascQueueTopChange, e.bottom.ID))

	e.ops.restack = &ascbridge.RestackStackResult{Items: []ascbridge.RestackItem{
		{Change: ascQueueBottomChange, State: ascbridge.RestackUnchanged}, {Change: ascQueueTopChange, State: ascbridge.RestackUnchanged}}}
	e.ops.revert = &ascbridge.RevertLandedResult{Reverts: []ascbridge.RevertedChange{{Original: ascQueueTopChange}, {Original: ascQueueBottomChange}}}

	yes := func(*repo_model.Repository) (bool, error) { return true, nil }
	t.Cleanup(automerge.UseAscQueueBacked(yes))
	t.Cleanup(pull_service.UseAscStackBacked(yes))
	t.Cleanup(pull_service.UseAscStackHead(func(_ context.Context, pr *issues_model.PullRequest) (string, error) {
		return fmt.Sprintf("%040d", pr.ID), nil
	}))
	t.Cleanup(automerge.UseAscQueueLander(e.lander))
	t.Cleanup(automerge.UseAscQueueKicker(func(int64, string) {}))
	t.Cleanup(ascbridge.UseStackOps(e.ops))
	return e
}

func ascQueueEntries(t *testing.T, repoID int64) []*automerge.AscStackQueueEntry {
	t.Helper()
	list, err := automerge.AscQueueList(t.Context(), repoID, "master")
	require.NoError(t, err)
	return list
}

func TestAscQueueStackPanelActions(t *testing.T) {
	defer tests.PrepareTestEnv(t)()
	e := newAscQueueEnv(t)
	session := loginUser(t, "user2")

	resp := session.MakeRequest(t, NewRequest(t, "GET", "/user2/repo1/pulls/5"), http.StatusOK)
	doc := NewHTMLParser(t, resp.Body)
	actions := doc.doc.Find(`[data-testid="asc-stack-actions"]`)
	require.Equal(t, 1, actions.Length(), "the owner sees the stack actions")
	for _, op := range []string{"op=queue", "op=merge", "op=restack"} {
		assert.Equal(t, 1, actions.Find(`a[href$="/user2/repo1/pulls/5/asc/confirm?`+op+`"]`).Length(), op)
	}
	assert.Equal(t, 0, actions.Find(`[data-testid="asc-unqueue-link"]`).Length())
	assert.Equal(t, 0, actions.Find(`[data-testid="asc-revert-link"]`).Length(), "nothing merged, nothing to revert")

	// A visitor sees the stack and no buttons.
	resp = MakeRequest(t, NewRequest(t, "GET", "/user2/repo1/pulls/5"), http.StatusOK)
	doc = NewHTMLParser(t, resp.Body)
	assert.Equal(t, 1, doc.doc.Find(`[data-asc-stack]`).Length())
	assert.Equal(t, 0, doc.doc.Find(`[data-testid="asc-stack-actions"]`).Length())

	// Nothing was written by looking.
	assert.Empty(t, ascQueueEntries(t, e.top.BaseRepoID))
	assert.Empty(t, e.lander.landed())
}

func TestAscQueueMergeWhenReady(t *testing.T) {
	defer tests.PrepareTestEnv(t)()
	e := newAscQueueEnv(t)
	session := loginUser(t, "user2")

	// The confirmation page is the dry run: it lists the stack and writes nothing.
	resp := session.MakeRequest(t, NewRequest(t, "GET", "/user2/repo1/pulls/5/asc/confirm?op=queue"), http.StatusOK)
	doc := NewHTMLParser(t, resp.Body)
	assert.Equal(t, 2, doc.doc.Find(`[data-testid="asc-rows"] tbody tr`).Length())
	form := doc.doc.Find(`form.asc-op-form`)
	action, _ := form.Attr("action")
	assert.Equal(t, "/user2/repo1/pulls/5/asc/enqueue", action)
	_, disabled := doc.doc.Find(`[data-testid="asc-confirm-button"]`).Attr("disabled")
	assert.False(t, disabled)
	assert.Empty(t, ascQueueEntries(t, e.top.BaseRepoID))

	// Queue it.
	resp = session.MakeRequest(t, NewRequestWithValues(t, "POST", action, nil), http.StatusSeeOther)
	assert.Equal(t, "/user2/repo1/pulls/5", test.RedirectURL(resp))
	entries := ascQueueEntries(t, e.top.BaseRepoID)
	require.Len(t, entries, 2)
	assert.Equal(t, ascQueueBottomChange, entries[0].AscChange, "bottom first")
	assert.Equal(t, automerge.AscQueueQueued, entries[0].State)

	// The pull request page shows the state of each entry and offers to remove it instead.
	resp = session.MakeRequest(t, NewRequest(t, "GET", "/user2/repo1/pulls/5"), http.StatusOK)
	doc = NewHTMLParser(t, resp.Body)
	assert.Equal(t, 2, doc.doc.Find(`[data-testid="asc-queue-state"]`).Length())
	assert.Equal(t, 1, doc.doc.Find(`[data-testid="asc-unqueue-link"]`).Length())
	assert.Equal(t, 0, doc.doc.Find(`[data-testid="asc-queue-link"]`).Length())

	// The queue page lists them in order.
	resp = session.MakeRequest(t, NewRequest(t, "GET", "/user2/repo1/asc/queue"), http.StatusOK)
	doc = NewHTMLParser(t, resp.Body)
	rows := doc.doc.Find(`[data-testid="asc-queue-active"] > li`)
	require.Equal(t, 2, rows.Length())
	assert.Contains(t, rows.First().Text(), "Queued")
	assert.Equal(t, 0, doc.doc.Find(`[data-testid="asc-queue-empty"]`).Length())

	// Remove it again: the confirmation names what leaves; the entries are cancelled.
	resp = session.MakeRequest(t, NewRequest(t, "GET", "/user2/repo1/pulls/5/asc/confirm?op=unqueue"), http.StatusOK)
	doc = NewHTMLParser(t, resp.Body)
	assert.Equal(t, 2, doc.doc.Find(`[data-testid="asc-rows"] tbody tr`).Length())
	session.MakeRequest(t, NewRequestWithValues(t, "POST", "/user2/repo1/pulls/5/asc/dequeue", nil), http.StatusSeeOther)
	for _, en := range ascQueueEntries(t, e.top.BaseRepoID) {
		assert.Equal(t, automerge.AscQueueCancelled, en.State)
	}
	resp = session.MakeRequest(t, NewRequest(t, "GET", "/user2/repo1/asc/queue"), http.StatusOK)
	doc = NewHTMLParser(t, resp.Body)
	assert.Equal(t, 1, doc.doc.Find(`[data-testid="asc-queue-empty"]`).Length())
}

// Security: who may do what, and what a stranger sees.
func TestAscQueueAuthorisation(t *testing.T) {
	defer tests.PrepareTestEnv(t)()
	e := newAscQueueEnv(t)

	// A visitor is sent to sign in by the router; nothing is queued, merged or paused.
	for _, path := range []string{"/user2/repo1/pulls/5/asc/confirm?op=queue"} {
		resp := MakeRequest(t, NewRequest(t, "GET", path), http.StatusSeeOther)
		assert.Contains(t, test.RedirectURL(resp), "/user/login", path)
	}
	for _, path := range []string{
		"/user2/repo1/pulls/5/asc/enqueue", "/user2/repo1/pulls/5/asc/dequeue", "/user2/repo1/pulls/5/asc/merge",
		"/user2/repo1/pulls/5/asc/restack", "/user2/repo1/pulls/5/asc/revert", "/user2/repo1/asc/queue/pause",
	} {
		resp := MakeRequest(t, NewRequestWithValues(t, "POST", path, map[string]string{"branch": "master"}), NoExpectedStatus)
		assert.NotEqual(t, http.StatusOK, resp.Code, path)
	}
	assert.Empty(t, ascQueueEntries(t, e.top.BaseRepoID))
	assert.Empty(t, e.lander.landed())

	// A reader may look at the queue and may not use it.
	reader := loginUser(t, "user5")
	reader.MakeRequest(t, NewRequest(t, "GET", "/user2/repo1/asc/queue"), http.StatusOK)
	reader.MakeRequest(t, NewRequestWithValues(t, "POST", "/user2/repo1/pulls/5/asc/enqueue", nil), http.StatusSeeOther)
	reader.MakeRequest(t, NewRequestWithValues(t, "POST", "/user2/repo1/pulls/5/asc/merge", nil), http.StatusSeeOther)
	reader.MakeRequest(t, NewRequestWithValues(t, "POST", "/user2/repo1/pulls/5/asc/restack", nil), http.StatusSeeOther)
	reader.MakeRequest(t, NewRequestWithValues(t, "POST", "/user2/repo1/pulls/5/asc/revert", nil), http.StatusSeeOther)
	reader.MakeRequest(t, NewRequestWithValues(t, "POST", "/user2/repo1/asc/queue/pause", map[string]string{"branch": "master"}), http.StatusForbidden)
	assert.Empty(t, ascQueueEntries(t, e.top.BaseRepoID), "refused, so nothing is queued")
	assert.Empty(t, e.lander.landed())
	paused, err := automerge.AscQueuePaused(t.Context(), e.top.BaseRepoID, "master")
	require.NoError(t, err)
	assert.False(t, paused)

	resp := reader.MakeRequest(t, NewRequest(t, "GET", "/user2/repo1/pulls/5"), http.StatusOK)
	doc := NewHTMLParser(t, resp.Body)
	assert.Equal(t, 0, doc.doc.Find(`[data-testid="asc-queue-link"]`).Length(), "a reader is not offered the button")
	assert.Equal(t, 0, doc.doc.Find(`[data-testid="asc-merge-link"]`).Length())

	// asc off: the pages and the routes are not there.
	owner := loginUser(t, "user2")
	defer test.MockVariableValue(&setting.Asc.Enabled, false)()
	owner.MakeRequest(t, NewRequest(t, "GET", "/user2/repo1/asc/queue"), http.StatusNotFound)
	owner.MakeRequest(t, NewRequest(t, "GET", "/user2/repo1/pulls/5/asc/confirm?op=queue"), http.StatusNotFound)
	owner.MakeRequest(t, NewRequestWithValues(t, "POST", "/user2/repo1/pulls/5/asc/enqueue", nil), http.StatusNotFound)
	assert.Empty(t, ascQueueEntries(t, e.top.BaseRepoID))
}

func TestAscQueueMergeNowAndPause(t *testing.T) {
	defer tests.PrepareTestEnv(t)()
	e := newAscQueueEnv(t)
	session := loginUser(t, "user2")

	// Merge now needs the stack to be ready: the page says what is not.
	resp := session.MakeRequest(t, NewRequest(t, "GET", "/user2/repo1/pulls/5/asc/confirm?op=merge"), http.StatusOK)
	doc := NewHTMLParser(t, resp.Body)
	assert.Equal(t, 2, doc.doc.Find(`[data-testid="asc-rows"] tbody tr`).Length())

	// Pause the branch: merge now is refused with the reason, and the queue page says paused.
	session.MakeRequest(t, NewRequestWithValues(t, "POST", "/user2/repo1/asc/queue/pause", map[string]string{"branch": "master"}), http.StatusSeeOther)
	resp = session.MakeRequest(t, NewRequest(t, "GET", "/user2/repo1/asc/queue"), http.StatusOK)
	doc = NewHTMLParser(t, resp.Body)
	assert.Contains(t, doc.doc.Find(`[data-testid="asc-queue-status"]`).Text(), "Paused")
	session.MakeRequest(t, NewRequestWithValues(t, "POST", "/user2/repo1/pulls/5/asc/merge", nil), http.StatusSeeOther)
	assert.Empty(t, e.lander.landed(), "a paused branch lands nothing")
	resp = session.MakeRequest(t, NewRequest(t, "GET", "/user2/repo1/pulls/5/asc/confirm?op=merge"), http.StatusOK)
	doc = NewHTMLParser(t, resp.Body)
	_, disabled := doc.doc.Find(`[data-testid="asc-confirm-button"]`).Attr("disabled")
	assert.True(t, disabled)

	// Resume and merge now: one land for the whole prefix.
	session.MakeRequest(t, NewRequestWithValues(t, "POST", "/user2/repo1/asc/queue/resume", map[string]string{"branch": "master"}), http.StatusSeeOther)
	session.MakeRequest(t, NewRequestWithValues(t, "POST", "/user2/repo1/pulls/5/asc/merge", nil), http.StatusSeeOther)
	assert.Equal(t, []int64{e.top.ID}, e.lander.landed())

	// A bad branch name is not a page.
	session.MakeRequest(t, NewRequest(t, "GET", "/user2/repo1/asc/queue?branch="+strings.Repeat("a", 300)), http.StatusNotFound)
}

func TestAscQueueRestack(t *testing.T) {
	defer tests.PrepareTestEnv(t)()
	e := newAscQueueEnv(t)
	session := loginUser(t, "user2")

	// Up to date: the page says so and the button is off.
	resp := session.MakeRequest(t, NewRequest(t, "GET", "/user2/repo1/pulls/5/asc/confirm?op=restack"), http.StatusOK)
	doc := NewHTMLParser(t, resp.Body)
	_, disabled := doc.doc.Find(`[data-testid="asc-confirm-button"]`).Attr("disabled")
	assert.True(t, disabled)

	// A conflict is shown with its paths, and the restack is refused.
	e.ops.restack = &ascbridge.RestackStackResult{Items: []ascbridge.RestackItem{
		{Change: ascQueueBottomChange, State: ascbridge.RestackConflict, Conflicts: []string{"src/main.go"}},
		{Change: ascQueueTopChange, State: ascbridge.RestackBlocked}}}
	resp = session.MakeRequest(t, NewRequest(t, "GET", "/user2/repo1/pulls/5/asc/confirm?op=restack"), http.StatusOK)
	doc = NewHTMLParser(t, resp.Body)
	assert.Contains(t, doc.doc.Find(`[data-testid="asc-rows"]`).Text(), "src/main.go")
	assert.Equal(t, 1, doc.doc.Find(`[data-testid="asc-failure"]`).Length())
	session.MakeRequest(t, NewRequestWithValues(t, "POST", "/user2/repo1/pulls/5/asc/restack", nil), http.StatusSeeOther)
	for _, c := range e.ops.calls {
		assert.Contains(t, c, "dry=true", "asc was never asked to write")
	}
}

func TestAscQueueRevertOpensAStackOfPullRequests(t *testing.T) {
	defer tests.PrepareTestEnv(t)()
	e := newAscQueueEnv(t)
	ctx := t.Context()
	repo := unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{ID: e.top.BaseRepoID})

	// Both pull requests have landed.
	_, err := db.GetEngine(ctx).Exec("UPDATE `pull_request` SET has_merged = ?, merged_commit_id = ? WHERE id IN (?, ?)", true, strings.Repeat("c", 40), e.bottom.ID, e.top.ID)
	require.NoError(t, err)

	// asc "stores" two reverts; their commits are real commits of the repository.
	master, err := git.GetFullCommitID(ctx, repo.RepoPath(), "master")
	require.NoError(t, err)
	branch2, err := git.GetFullCommitID(ctx, repo.RepoPath(), "branch2")
	require.NoError(t, err)
	// the head of the second revert needs a first parent (the stack rule reads it), which master, the root commit, has not
	branch2Parent, err := git.GetFullCommitID(ctx, repo.RepoPath(), "branch2~1")
	require.NoError(t, err)
	e.ops.revert = &ascbridge.RevertLandedResult{Ref: "refs/users/user2/reverts", Applied: true, StackTop: "nnnnnnnnnnnnnnnnnnnnnnnnnnnnnnnn", Reverts: []ascbridge.RevertedChange{
		{Original: ascQueueTopChange, RevertChange: "mmmmmmmmmmmmmmmmmmmmmmmmmmmmmmmm", RevertGit: branch2},
		{Original: ascQueueBottomChange, RevertChange: "nnnnnnnnnnnnnnnnnnnnnnnnnnnnnnnn", RevertGit: branch2Parent},
	}}

	session := loginUser(t, "user2")
	resp := session.MakeRequest(t, NewRequest(t, "GET", "/user2/repo1/pulls/5"), http.StatusOK)
	doc := NewHTMLParser(t, resp.Body)
	assert.Equal(t, 1, doc.doc.Find(`[data-testid="asc-revert-link"]`).Length(), "a merged pull request offers the revert")
	assert.Equal(t, 0, doc.doc.Find(`[data-testid="asc-queue-link"]`).Length())

	// The dry run lists what would be reverted.
	resp = session.MakeRequest(t, NewRequest(t, "GET", "/user2/repo1/pulls/5/asc/confirm?op=revert"), http.StatusOK)
	doc = NewHTMLParser(t, resp.Body)
	assert.Equal(t, 2, doc.doc.Find(`[data-testid="asc-rows"] tbody tr`).Length())
	before := unittest.GetCount(t, &issues_model.PullRequest{})

	resp = session.MakeRequest(t, NewRequestWithValues(t, "POST", "/user2/repo1/pulls/5/asc/revert", map[string]string{"scope": "stack"}), http.StatusSeeOther)
	assert.Equal(t, before+2, unittest.GetCount(t, &issues_model.PullRequest{}), "one pull request per revert")

	var fresh []*issues_model.PullRequest
	require.NoError(t, db.GetEngine(ctx).Where("id > ?", e.top.ID).OrderBy("id").Find(&fresh))
	require.GreaterOrEqual(t, len(fresh), 2)
	first, second := fresh[len(fresh)-2], fresh[len(fresh)-1]
	assert.Equal(t, issues_model.PullRequestFlowAGit, first.Flow)
	assert.Equal(t, "master", first.BaseBranch)
	assert.Equal(t, "user2/asc-mmmmmmmmmmmmmmmmmmmmmmmmmmmmmmmm", first.HeadBranch, "the head commit is not stored; the branch names the revert")
	row, err := issues_model.GetPullRequestAscStack(ctx, first.ID)
	require.NoError(t, err)
	require.NotNil(t, row)
	assert.Equal(t, "mmmmmmmmmmmmmmmmmmmmmmmmmmmmmmmm", row.AscChange)
	assert.EqualValues(t, 0, row.StackParentID)
	row, err = issues_model.GetPullRequestAscStack(ctx, second.ID)
	require.NoError(t, err)
	require.NotNil(t, row)
	assert.Equal(t, first.ID, row.StackParentID, "stacked in order")
	require.NoError(t, second.LoadIssue(ctx))
	assert.Equal(t, "/user2/repo1/pulls/"+fmt.Sprint(second.Issue.Index), test.RedirectURL(resp), "the page goes to the top of the new stack")
	assert.Contains(t, second.Issue.Title, "Revert")

	// Nothing wrote the target branch: the repository still has the same master.
	after, err := git.GetFullCommitID(ctx, repo.RepoPath(), "master")
	require.NoError(t, err)
	assert.Equal(t, master, after)
}
