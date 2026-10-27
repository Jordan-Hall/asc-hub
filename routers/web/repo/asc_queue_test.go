// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	gocontext "context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"forgejo.org/models"
	"forgejo.org/models/db"
	issues_model "forgejo.org/models/issues"
	access_model "forgejo.org/models/perm/access"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/test"
	"forgejo.org/modules/web/middleware"
	"forgejo.org/services/ascbridge"
	"forgejo.org/services/automerge"
	"forgejo.org/services/context"
	"forgejo.org/services/contexttest"
	pull_service "forgejo.org/services/pull"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The relay answers only what ascRPCReadMethods lists. Stacks and MergeQueue are served by the hub's own handlers
// (asc_queue.go and friends), which check Forgejo permissions first, so none of their methods is on the list.
func init() {
	for _, name := range []string{
		"asc.v1.Stacks/Restack", "asc.v1.Stacks/RevertLanded", "asc.v1.Stacks/ListRefLog", "asc.v1.Stacks/UndoRef",
		"asc.v1.MergeQueue/Enqueue", "asc.v1.MergeQueue/Dequeue", "asc.v1.MergeQueue/List", "asc.v1.MergeQueue/Get",
		"asc.v1.MergeQueue/Revalidate", "asc.v1.MergeQueue/SetPaused", "asc.v1.MergeQueue/Watch",
	} {
		ascRPCTestDenied[name] = "served by the hub's own stack and queue handlers"
	}
}

const (
	qrBottomChange = "kkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkk"
	qrTopChange    = "llllllllllllllllllllllllllllllll"
)

func TestAscOpError(t *testing.T) {
	for _, tc := range []struct {
		err    error
		key    string
		status int
	}{
		{pull_service.ErrAscStackNotStacked, "not_stacked", http.StatusNotFound},
		{automerge.ErrAscQueueNotAllowed, "not_allowed", http.StatusForbidden},
		{automerge.ErrAscQueueNoPermission, "not_allowed", http.StatusForbidden},
		{pull_service.ErrAscStackNotAllowed, "not_allowed", http.StatusForbidden},
		{pull_service.ErrAscRestackNotAllowed, "not_allowed", http.StatusForbidden},
		{pull_service.ErrAscRevertNotAllowed, "not_allowed", http.StatusForbidden},
		{ascbridge.ErrStackDenied, "not_allowed", http.StatusForbidden},
		{automerge.ErrAscQueueDisabled, "disabled", 0},
		{pull_service.ErrAscStackDisabled, "disabled", 0},
		{pull_service.ErrAscRevertDisabled, "disabled", 0},
		{ascbridge.ErrStacksUnavailable, "unavailable", 0},
		{automerge.ErrAscQueuePaused, "paused", 0},
		{automerge.ErrAscQueueBusy, "busy", 0},
		{pull_service.ErrAscRevertBusy, "revert_busy", 0},
		{pull_service.ErrAscLandNeedsContentMerge, "needs_content_merge", 0},
		{automerge.ErrAscLandNeedsContentMerge, "needs_content_merge", 0},
		{pull_service.ErrAscStackHeadMoved, "head_moved", 0},
		{pull_service.ErrAscRestackNotOpen, "not_open", 0},
		{pull_service.ErrAscRestackNoPull, "no_pull", 0},
		{pull_service.ErrAscRevertNotLanded, "not_landed", 0},
		{ascbridge.ErrStackNotLanded, "not_landed", 0},
		{pull_service.ErrAscRevertBottom, "bad_bottom", http.StatusNotFound},
		{ascbridge.ErrStackFrozen, "frozen", 0},
		{ascbridge.ErrStackNotFound, "asc_not_found", 0},
		{ascbridge.ErrStackBadRequest, "asc_refused", 0},
		{ascbridge.ErrBadAnswer, "bad_answer", 0},
		{pull_service.ErrAscRevertBadAnswer, "bad_answer", 0},
		{fmt.Errorf("wrapped: %w", ascbridge.ErrStackFrozen), "frozen", 0},
	} {
		f := ascOpError(tc.err)
		assert.True(t, f.Known, tc.err.Error())
		assert.Equal(t, "repo.pulls.asc_op.err."+tc.key, f.Key, tc.err.Error())
		assert.Equal(t, tc.status, f.Status, tc.err.Error())
		assert.Empty(t, f.Args, "no text of the error is passed on: %s", tc.err)
	}

	// A conflict names its paths; the revert conflict has its own text.
	f := ascOpError(&ascbridge.ConflictError{Err: ascbridge.ErrStackConflict, Paths: []string{"a.go", "b.go"}})
	assert.Equal(t, "repo.pulls.asc_op.err.conflict", f.Key)
	assert.Equal(t, []any{"a.go, b.go"}, f.Args)
	f = ascOpError(&ascbridge.ConflictError{Err: fmt.Errorf("RevertLanded: %w", ascbridge.ErrRevertConflict), Paths: []string{"c.go"}})
	assert.Equal(t, "repo.pulls.asc_op.err.revert_conflict", f.Key)

	// Not ready carries our own text with the pull request number.
	f = ascOpError(fmt.Errorf("%w: #4 is a draft", pull_service.ErrAscStackNotMergable))
	assert.True(t, f.Known)
	assert.Equal(t, "repo.pulls.asc_op.err.not_ready", f.Key)
	require.Len(t, f.Args, 1)
	assert.Contains(t, f.Args[0], "#4 is a draft")
	f = ascOpError(fmt.Errorf("%w: #4 is a draft", automerge.ErrAscQueueNotQueueable))
	assert.Equal(t, "repo.pulls.asc_op.err.not_ready", f.Key)

	// What is not known is a server error, and none of its text becomes a message.
	f = ascOpError(errors.New("pq: password authentication failed for user forgejo"))
	assert.False(t, f.Known)
	assert.Empty(t, f.Args)
	assert.False(t, ascOpError(nil).Known)
}

func TestAscPathsArgIsBounded(t *testing.T) {
	assert.Equal(t, "a, b", ascPathsArg([]string{"a", "b"}))
	assert.Equal(t, "", ascPathsArg(nil))
	many := []string{"1", "2", "3", "4", "5", "6", "7"}
	assert.Equal(t, "1, 2, 3, 4, 5, …", ascPathsArg(many))
	assert.Len(t, many, 7, "the caller's slice is not changed")
}

func TestAscValidBranch(t *testing.T) {
	for _, ok := range []string{"main", "feature/x", "release-1.2", "ünï", strings.Repeat("a", ascMaxBranchBytes)} {
		assert.True(t, ascValidBranch(ok), ok)
	}
	for _, bad := range []string{"", strings.Repeat("a", ascMaxBranchBytes+1), "a\x00b", "a\nb", "a\x7fb", "\xff\xfe"} {
		assert.False(t, ascValidBranch(bad), "%q", bad)
	}
}

func TestAscCheckReason(t *testing.T) {
	assert.Equal(t, "Not all required status checks successful",
		ascCheckReason(fmt.Errorf("%w: #3: %w", pull_service.ErrAscStackNotMergable, models.ErrDisallowedToMerge{Reason: "Not all required status checks successful"})))
	assert.Equal(t, "#3 is a draft", ascCheckReason(fmt.Errorf("%w: #3 is a draft", pull_service.ErrAscStackNotMergable)))
	assert.Equal(t, "#3", ascCheckReason(fmt.Errorf("%w: #3", pull_service.ErrAscStackHeadMoved)))
	assert.Equal(t, "#3", ascCheckReason(fmt.Errorf("%w: #3", pull_service.ErrAscStackNotAllowed)))
	assert.Empty(t, ascCheckReason(errors.New("pq: connection refused")), "an error that is not ours gives no text")
	assert.Equal(t, "abcdefghijkl", ascShortChange(qrBottomChange[:0]+"abcdefghijklmnop"))
	assert.Equal(t, "abc", ascShortChange("abc"))
}

// ---------------------------------------------------------------------------------- handlers

type qrFixture struct {
	bottom, top *issues_model.PullRequest
	lander      *qrLander
	ops         *qrOps
}

func (f *qrFixture) link(pr *issues_model.PullRequest) string {
	return fmt.Sprintf("/user2/repo1/pulls/%d", pr.Index)
}

type qrLander struct {
	lands    []int64
	landErr  error
	restacks int
}

func (l *qrLander) Land(_ gocontext.Context, _ *user_model.User, _ access_model.Permission, pr *issues_model.PullRequest, _ pull_service.AscMergeOptions) (*pull_service.AscStackMergeResult, error) {
	l.lands = append(l.lands, pr.ID)
	if l.landErr != nil {
		return nil, l.landErr
	}
	return &pull_service.AscStackMergeResult{Merged: []*issues_model.PullRequest{pr}}, nil
}

func (l *qrLander) Restack(gocontext.Context, *user_model.User, *issues_model.PullRequest) error {
	l.restacks++
	return nil
}

type qrOps struct {
	restackDry *ascbridge.RestackStackResult
	revertDry  *ascbridge.RevertLandedResult
	reqs       int
}

func (o *qrOps) RestackStack(_ gocontext.Context, req ascbridge.RestackStackRequest) (*ascbridge.RestackStackResult, error) {
	o.reqs++
	return o.restackDry, nil
}

func (o *qrOps) RevertLanded(_ gocontext.Context, req ascbridge.RevertLandedRequest) (*ascbridge.RevertLandedResult, error) {
	o.reqs++
	return o.revertDry, nil
}

// newQRFixture links pull requests 2 (bottom) and 5 (top) of repository 1 as a stack on master and fakes everything
// that would call asc: the lander, the Stacks client, the heads and the test that the repository is in asc.
func newQRFixture(t *testing.T) *qrFixture {
	t.Helper()
	require.NoError(t, unittest.PrepareTestDatabase())
	t.Cleanup(test.MockVariableValue(&setting.Asc.Enabled, true))
	ctx := t.Context()
	f := &qrFixture{lander: &qrLander{}, ops: &qrOps{}}
	f.bottom = unittest.AssertExistsAndLoadBean(t, &issues_model.PullRequest{ID: 2})
	f.top = unittest.AssertExistsAndLoadBean(t, &issues_model.PullRequest{ID: 5})
	_, err := db.GetEngine(ctx).Exec("UPDATE `pull_request` SET base_branch = ? WHERE id = ?", "master", f.top.ID)
	require.NoError(t, err)
	f.top.BaseBranch = "master"
	require.NoError(t, issues_model.SetPullRequestAscStack(ctx, f.bottom, qrBottomChange, 0))
	require.NoError(t, issues_model.SetPullRequestAscStack(ctx, f.top, qrTopChange, f.bottom.ID))

	f.ops.restackDry = &ascbridge.RestackStackResult{Items: []ascbridge.RestackItem{
		{Change: qrBottomChange, State: ascbridge.RestackUnchanged}, {Change: qrTopChange, State: ascbridge.RestackUnchanged}}}
	f.ops.revertDry = &ascbridge.RevertLandedResult{Reverts: []ascbridge.RevertedChange{{Original: qrTopChange}, {Original: qrBottomChange}}}

	yes := func(*repo_model.Repository) (bool, error) { return true, nil }
	t.Cleanup(automerge.UseAscQueueBacked(yes))
	t.Cleanup(pull_service.UseAscStackBacked(yes))
	t.Cleanup(pull_service.UseAscStackHead(func(_ gocontext.Context, pr *issues_model.PullRequest) (string, error) {
		return fmt.Sprintf("%040d", pr.ID), nil
	}))
	t.Cleanup(automerge.UseAscQueueLander(f.lander))
	t.Cleanup(automerge.UseAscQueueKicker(func(int64, string) {}))
	t.Cleanup(ascbridge.UseStackOps(f.ops))
	return f
}

// qrRequest runs h as the user (0: nobody) on a path of repository 1 with the given form values.
func qrRequest(t *testing.T, h func(*context.Context), userID int64, pr *issues_model.PullRequest, path string, form url.Values) (*context.Context, *httptest.ResponseRecorder) {
	t.Helper()
	ctx, resp := contexttest.MockContext(t, path)
	ctx.Flash = &middleware.Flash{DataStore: ctx, Values: url.Values{}}
	if userID != 0 {
		contexttest.LoadUser(t, ctx, userID)
		ctx.IsSigned = true
	}
	contexttest.LoadRepo(t, ctx, 1)
	if pr != nil {
		ctx.SetParams(":index", fmt.Sprint(pr.Index))
	}
	for k, v := range form {
		ctx.Req.Form[k] = v
	}
	h(ctx)
	return ctx, resp
}

func TestAscStackEndpointsRefuseEarly(t *testing.T) {
	f := newQRFixture(t)
	handlers := map[string]func(*context.Context){
		"enqueue": AscQueueEnqueue, "dequeue": AscQueueDequeue, "merge": AscQueueMergeNow,
		"restack": AscStackRestack, "revert": AscStackRevert, "confirm": AscConfirm,
	}
	for name, h := range handlers {
		_, resp := qrRequest(t, h, 0, f.top, "user2/repo1/pulls/5/asc/"+name, url.Values{"op": {"queue"}})
		assert.Equal(t, http.StatusForbidden, resp.Code, "%s: signed out", name)
	}
	for name, h := range handlers {
		func() {
			defer test.MockVariableValue(&setting.Asc.Enabled, false)()
			_, resp := qrRequest(t, h, 2, f.top, "user2/repo1/pulls/5/asc/"+name, url.Values{"op": {"queue"}})
			assert.Equal(t, http.StatusNotFound, resp.Code, "%s: asc off", name)
		}()
	}
	assert.Empty(t, f.lander.lands)
}

func TestAscEnqueueDequeueHandlers(t *testing.T) {
	f := newQRFixture(t)

	// A user who may not merge is refused by the service; the page shows why and nothing is queued.
	ctx, resp := qrRequest(t, AscQueueEnqueue, 5, f.top, "user2/repo1/pulls/5/asc/enqueue", nil)
	assert.Equal(t, http.StatusSeeOther, resp.Code)
	assert.Equal(t, f.link(f.top), resp.Header().Get("Location"))
	assert.Equal(t, "repo.pulls.asc_op.err.not_allowed", ctx.Flash.ErrorMsg)
	list, err := automerge.AscQueueList(t.Context(), f.top.BaseRepoID, "master")
	require.NoError(t, err)
	assert.Empty(t, list)

	// The owner queues the stack from the bottom up to the top.
	ctx, resp = qrRequest(t, AscQueueEnqueue, 2, f.top, "user2/repo1/pulls/5/asc/enqueue", nil)
	assert.Equal(t, http.StatusSeeOther, resp.Code)
	assert.Equal(t, "repo.pulls.asc_op.queued", ctx.Flash.SuccessMsg)
	list, err = automerge.AscQueueList(t.Context(), f.top.BaseRepoID, "master")
	require.NoError(t, err)
	require.Len(t, list, 2)
	assert.Equal(t, automerge.AscQueueQueued, list[0].State)

	// Someone else may not take the owner's entry out.
	ctx, resp = qrRequest(t, AscQueueDequeue, 5, f.top, "user2/repo1/pulls/5/asc/dequeue", nil)
	assert.Equal(t, http.StatusSeeOther, resp.Code)
	assert.Equal(t, "repo.pulls.asc_op.err.not_allowed", ctx.Flash.ErrorMsg)
	list, err = automerge.AscQueueList(t.Context(), f.top.BaseRepoID, "master")
	require.NoError(t, err)
	assert.Equal(t, automerge.AscQueueQueued, list[1].State)

	// The owner can.
	ctx, resp = qrRequest(t, AscQueueDequeue, 2, f.top, "user2/repo1/pulls/5/asc/dequeue", nil)
	assert.Equal(t, http.StatusSeeOther, resp.Code)
	assert.Equal(t, "repo.pulls.asc_op.dequeued", ctx.Flash.SuccessMsg)
	list, err = automerge.AscQueueList(t.Context(), f.top.BaseRepoID, "master")
	require.NoError(t, err)
	assert.Equal(t, automerge.AscQueueCancelled, list[1].State)

	// A repository that is not in asc has no queue.
	defer automerge.UseAscQueueBacked(func(*repo_model.Repository) (bool, error) { return false, nil })()
	ctx, _ = qrRequest(t, AscQueueEnqueue, 2, f.top, "user2/repo1/pulls/5/asc/enqueue", nil)
	assert.Equal(t, "repo.pulls.asc_op.err.disabled", ctx.Flash.ErrorMsg)
}

func TestAscMergeNowHandler(t *testing.T) {
	f := newQRFixture(t)

	ctx, resp := qrRequest(t, AscQueueMergeNow, 2, f.top, "user2/repo1/pulls/5/asc/merge", nil)
	assert.Equal(t, http.StatusSeeOther, resp.Code)
	assert.Equal(t, "repo.pulls.asc_op.merged", ctx.Flash.SuccessMsg)
	assert.Equal(t, []int64{f.top.ID}, f.lander.lands)

	// A land that would need a content merge is explained, not retried.
	f.lander.landErr = pull_service.ErrAscLandNeedsContentMerge
	ctx, resp = qrRequest(t, AscQueueMergeNow, 2, f.top, "user2/repo1/pulls/5/asc/merge", nil)
	assert.Equal(t, http.StatusSeeOther, resp.Code)
	assert.Equal(t, "repo.pulls.asc_op.err.needs_content_merge", ctx.Flash.ErrorMsg)
	assert.Equal(t, 0, f.lander.restacks, "merging now never restacks by itself")

	// A paused branch lands nothing.
	f.lander.landErr = nil
	require.NoError(t, f.top.LoadBaseRepo(t.Context()))
	require.NoError(t, automerge.AscQueueSetPaused(t.Context(), qrUser(t, 2), f.top.BaseRepo, "master", true))
	n := len(f.lander.lands)
	ctx, _ = qrRequest(t, AscQueueMergeNow, 2, f.top, "user2/repo1/pulls/5/asc/merge", nil)
	assert.Equal(t, "repo.pulls.asc_op.err.paused", ctx.Flash.ErrorMsg)
	assert.Len(t, f.lander.lands, n)
}

func TestAscRestackAndRevertHandlers(t *testing.T) {
	f := newQRFixture(t)

	// The stack is on the target: a restack says so and writes nothing.
	ctx, resp := qrRequest(t, AscStackRestack, 2, f.top, "user2/repo1/pulls/5/asc/restack", nil)
	assert.Equal(t, http.StatusSeeOther, resp.Code)
	assert.Equal(t, "repo.pulls.asc_op.restack_up_to_date", ctx.Flash.InfoMsg)
	assert.Equal(t, 1, f.ops.reqs, "only the dry run")

	// Without the right to update the pull requests the whole call is refused.
	f.ops.restackDry = &ascbridge.RestackStackResult{Items: []ascbridge.RestackItem{
		{Change: qrBottomChange, State: ascbridge.RestackRewritten}, {Change: qrTopChange, State: ascbridge.RestackRewritten}}}
	ctx, _ = qrRequest(t, AscStackRestack, 5, f.top, "user2/repo1/pulls/5/asc/restack", nil)
	assert.Equal(t, "repo.pulls.asc_op.err.not_allowed", ctx.Flash.ErrorMsg)
	assert.Equal(t, 1, f.ops.reqs, "a person who may not rewrite the pull request is refused before asc is asked anything")

	// A conflict is reported with its paths.
	f.ops.restackDry = &ascbridge.RestackStackResult{Items: []ascbridge.RestackItem{
		{Change: qrBottomChange, State: ascbridge.RestackConflict, Conflicts: []string{"a.go"}}, {Change: qrTopChange, State: ascbridge.RestackBlocked}}}
	ctx, _ = qrRequest(t, AscStackRestack, 2, f.top, "user2/repo1/pulls/5/asc/restack", nil)
	assert.Equal(t, "repo.pulls.asc_op.err.conflict", ctx.Flash.ErrorMsg)

	// Revert: a bad scope is a 400, an open pull request is not landed, a reader may not.
	_, resp = qrRequest(t, AscStackRevert, 2, f.top, "user2/repo1/pulls/5/asc/revert", url.Values{"scope": {"everything"}})
	assert.Equal(t, http.StatusBadRequest, resp.Code)
	ctx, resp = qrRequest(t, AscStackRevert, 2, f.top, "user2/repo1/pulls/5/asc/revert", nil)
	assert.Equal(t, http.StatusSeeOther, resp.Code)
	assert.Equal(t, "repo.pulls.asc_op.err.not_landed", ctx.Flash.ErrorMsg)
	ctx, _ = qrRequest(t, AscStackRevert, 5, f.top, "user2/repo1/pulls/5/asc/revert", nil)
	assert.Equal(t, "repo.pulls.asc_op.err.not_allowed", ctx.Flash.ErrorMsg)
}

func TestAscConfirmPage(t *testing.T) {
	f := newQRFixture(t)
	confirm := func(userID int64, op string, extra ...string) (*context.Context, *httptest.ResponseRecorder) {
		form := url.Values{"op": {op}}
		for i := 0; i+1 < len(extra); i += 2 {
			form.Set(extra[i], extra[i+1])
		}
		return qrRequest(t, AscConfirm, userID, f.top, "user2/repo1/pulls/5/asc/confirm", form)
	}
	rows := func(ctx *context.Context) []AscConfirmRow {
		r, _ := ctx.Data["AscRows"].([]AscConfirmRow)
		return r
	}

	// Merge when ready: every pull request is listed; nothing is written.
	ctx, resp := confirm(2, "queue")
	assert.Equal(t, http.StatusOK, resp.Code)
	assert.Equal(t, "queue", ctx.Data["AscOp"])
	assert.Equal(t, "/user2/repo1/pulls/5/asc/enqueue", ctx.Data["AscAction"])
	require.Len(t, rows(ctx), 2)
	assert.Equal(t, "ready", rows(ctx)[0].Status)
	assert.Equal(t, true, ctx.Data["AscCanConfirm"])
	assert.Equal(t, "ready", rows(ctx)[1].Status)
	list, err := automerge.AscQueueList(t.Context(), f.top.BaseRepoID, "master")
	require.NoError(t, err)
	assert.Empty(t, list)

	// Merge now needs everything ready: a pull request that is not ready disables the button and says why.
	_, err = db.GetEngine(t.Context()).Exec("UPDATE `issue` SET is_closed = ? WHERE id = ?", true, f.bottom.IssueID)
	require.NoError(t, err)
	ctx, resp = confirm(2, "merge")
	assert.Equal(t, http.StatusOK, resp.Code)
	assert.Equal(t, false, ctx.Data["AscCanConfirm"])
	assert.Equal(t, "not_ready", rows(ctx)[0].Status)
	assert.NotEmpty(t, rows(ctx)[0].Detail)
	// ...while merge when ready may still be asked for.
	ctx, _ = confirm(2, "queue")
	assert.Equal(t, true, ctx.Data["AscCanConfirm"])
	_, err = db.GetEngine(t.Context()).Exec("UPDATE `issue` SET is_closed = ? WHERE id = ?", false, f.bottom.IssueID)
	require.NoError(t, err)

	// Remove from the queue lists what is queued.
	_, err = automerge.AscQueueEnqueue(t.Context(), qrUser(t, 2), f.top)
	require.NoError(t, err)
	ctx, resp = confirm(2, "unqueue")
	assert.Equal(t, http.StatusOK, resp.Code)
	require.Len(t, rows(ctx), 2)
	assert.Equal(t, "queued_queued", rows(ctx)[1].Status)
	assert.Equal(t, true, ctx.Data["AscCanConfirm"])
	ctx, _ = confirm(2, "queue")
	assert.Equal(t, "queued_queued", rows(ctx)[0].Status, "a queued pull request shows as queued")

	// Restack: the preview names the changes and their states.
	f.ops.restackDry = &ascbridge.RestackStackResult{Items: []ascbridge.RestackItem{
		{Change: qrBottomChange, State: ascbridge.RestackRewritten}, {Change: qrTopChange, State: ascbridge.RestackRewritten}}}
	ctx, resp = confirm(2, "restack")
	assert.Equal(t, http.StatusOK, resp.Code)
	assert.Equal(t, "/user2/repo1/pulls/5/asc/restack", ctx.Data["AscAction"])
	require.Len(t, rows(ctx), 2)
	assert.Equal(t, "rewritten", rows(ctx)[0].Status)
	assert.Equal(t, true, ctx.Data["AscCanConfirm"])
	f.ops.restackDry = &ascbridge.RestackStackResult{Items: []ascbridge.RestackItem{
		{Change: qrBottomChange, State: ascbridge.RestackConflict, Conflicts: []string{"a.go"}}, {Change: qrTopChange, State: ascbridge.RestackBlocked}}}
	ctx, _ = confirm(2, "restack")
	assert.Equal(t, false, ctx.Data["AscCanConfirm"])
	assert.NotEmpty(t, ctx.Data["AscFailure"])
	assert.Equal(t, "a.go", rows(ctx)[0].Detail)

	// A reader gets the refusal on the page, not an action.
	ctx, resp = confirm(5, "restack")
	assert.Equal(t, http.StatusOK, resp.Code)
	assert.Equal(t, false, ctx.Data["AscCanConfirm"])
	assert.EqualValues(t, "repo.pulls.asc_op.err.not_allowed", ctx.Data["AscFailure"])

	// Anything else is a 404.
	for _, op := range []string{"", "delete", "QUEUE", "queue/../x"} {
		_, resp = confirm(2, op)
		assert.Equal(t, http.StatusNotFound, resp.Code, "op %q", op)
	}
}

func TestAscConfirmRevert(t *testing.T) {
	f := newQRFixture(t)
	_, err := db.GetEngine(t.Context()).Exec("UPDATE `pull_request` SET has_merged = ?, merged_commit_id = ? WHERE id IN (?, ?)", true, strings.Repeat("c", 40), f.bottom.ID, f.top.ID)
	require.NoError(t, err)
	f.top = unittest.AssertExistsAndLoadBean(t, &issues_model.PullRequest{ID: f.top.ID})

	confirm := func(form url.Values) (*context.Context, *httptest.ResponseRecorder) {
		form.Set("op", "revert")
		return qrRequest(t, AscConfirm, 2, f.top, "user2/repo1/pulls/5/asc/confirm", form)
	}
	ctx, resp := confirm(url.Values{})
	assert.Equal(t, http.StatusOK, resp.Code)
	assert.Equal(t, "/user2/repo1/pulls/5/asc/revert", ctx.Data["AscAction"])
	assert.Equal(t, false, ctx.Data["AscScopeThis"])
	rows, _ := ctx.Data["AscRows"].([]AscConfirmRow)
	require.Len(t, rows, 2)
	assert.Equal(t, "revert", rows[0].Status)
	assert.Equal(t, true, ctx.Data["AscCanConfirm"])

	// A conflict in the preview disables the button.
	f.ops.revertDry = &ascbridge.RevertLandedResult{Reverts: []ascbridge.RevertedChange{
		{Original: qrTopChange, Conflicts: []string{"x.go"}}, {Original: qrBottomChange}}}
	ctx, _ = confirm(url.Values{})
	assert.Equal(t, false, ctx.Data["AscCanConfirm"])
	assert.NotEmpty(t, ctx.Data["AscFailure"])
	rows, _ = ctx.Data["AscRows"].([]AscConfirmRow)
	assert.Equal(t, "conflict", rows[0].Status)
	assert.Equal(t, "x.go", rows[0].Detail)

	// The scope and squash options come back on the page; a bad scope is a 400.
	f.ops.revertDry = &ascbridge.RevertLandedResult{Reverts: []ascbridge.RevertedChange{{Original: qrTopChange}}}
	ctx, _ = confirm(url.Values{"scope": {"this"}, "squash": {"1"}})
	assert.Equal(t, true, ctx.Data["AscScopeThis"])
	assert.Equal(t, true, ctx.Data["AscSquash"])
	_, resp = confirm(url.Values{"scope": {"bogus"}})
	assert.Equal(t, http.StatusBadRequest, resp.Code)
}

func TestAscQueuePage(t *testing.T) {
	f := newQRFixture(t)
	page := func(userID int64, query string) (*context.Context, *httptest.ResponseRecorder) {
		return qrRequest(t, AscQueuePage, userID, nil, "user2/repo1/asc/queue"+query, nil)
	}

	_, err := automerge.AscQueueEnqueue(t.Context(), qrUser(t, 2), f.top)
	require.NoError(t, err)

	ctx, resp := page(2, "")
	assert.Equal(t, http.StatusOK, resp.Code)
	assert.Equal(t, "master", ctx.Data["AscQueueBranch"], "the default branch of repository 1")
	view, ok := ctx.Data["AscQueue"].(*automerge.AscQueueView)
	require.True(t, ok)
	assert.Len(t, view.Active, 2)
	assert.Equal(t, true, ctx.Data["AscQueueCanAdmin"])
	users, _ := ctx.Data["AscQueueUsers"].(map[int64]*user_model.User)
	assert.NotNil(t, users[2])

	// A visitor and a reader see the queue and cannot administer it.
	ctx, resp = page(0, "")
	assert.Equal(t, http.StatusOK, resp.Code)
	assert.Equal(t, false, ctx.Data["AscQueueCanAdmin"])
	ctx, _ = page(5, "?branch=master")
	assert.Equal(t, false, ctx.Data["AscQueueCanAdmin"])

	// An empty queue of another branch.
	ctx, resp = page(2, "?branch=other")
	assert.Equal(t, http.StatusOK, resp.Code)
	view, _ = ctx.Data["AscQueue"].(*automerge.AscQueueView)
	assert.Empty(t, view.Active)

	// A bad branch, asc off, or a repository that is not in asc: 404.
	for _, q := range []string{"?branch=" + url.QueryEscape("a\x00b"), "?branch=" + url.QueryEscape(strings.Repeat("a", 300))} {
		_, resp = page(2, q)
		assert.Equal(t, http.StatusNotFound, resp.Code, q)
	}
	func() {
		defer test.MockVariableValue(&setting.Asc.Enabled, false)()
		_, resp := page(2, "")
		assert.Equal(t, http.StatusNotFound, resp.Code)
	}()
	defer automerge.UseAscQueueBacked(func(*repo_model.Repository) (bool, error) { return false, nil })()
	_, resp = page(2, "")
	assert.Equal(t, http.StatusNotFound, resp.Code)
}

func TestAscQueuePauseResume(t *testing.T) {
	f := newQRFixture(t)
	pause := func(h func(*context.Context), userID int64, branch string) (*context.Context, *httptest.ResponseRecorder) {
		return qrRequest(t, h, userID, nil, "POST user2/repo1/asc/queue/pause", url.Values{"branch": {branch}})
	}
	paused := func() bool {
		p, err := automerge.AscQueuePaused(t.Context(), f.top.BaseRepoID, "master")
		require.NoError(t, err)
		return p
	}

	_, resp := pause(AscQueuePause, 0, "master")
	assert.Equal(t, http.StatusForbidden, resp.Code)
	_, resp = pause(AscQueuePause, 5, "master")
	assert.Equal(t, http.StatusForbidden, resp.Code, "only an administrator may pause")
	assert.False(t, paused())
	_, resp = pause(AscQueuePause, 2, "bad\x00branch")
	assert.Equal(t, http.StatusNotFound, resp.Code)

	ctx, resp := pause(AscQueuePause, 2, "master")
	assert.Equal(t, http.StatusSeeOther, resp.Code)
	assert.Equal(t, "/user2/repo1/asc/queue?branch=master", resp.Header().Get("Location"))
	assert.Equal(t, "repo.asc_queue.paused_done", ctx.Flash.SuccessMsg)
	assert.True(t, paused())

	_, resp = pause(AscQueueResume, 5, "master")
	assert.Equal(t, http.StatusForbidden, resp.Code)
	assert.True(t, paused())
	ctx, resp = pause(AscQueueResume, 1, "master")
	assert.Equal(t, http.StatusSeeOther, resp.Code, "a site administrator may")
	assert.Equal(t, "repo.asc_queue.resumed_done", ctx.Flash.SuccessMsg)
	assert.False(t, paused())
}

func TestPrepareAscStackActions(t *testing.T) {
	f := newQRFixture(t)
	actions := func(userID int64, pr *issues_model.PullRequest) *AscStackActions {
		ctx, _ := contexttest.MockContext(t, "user2/repo1/pulls/5")
		if userID != 0 {
			contexttest.LoadUser(t, ctx, userID)
			ctx.IsSigned = true
		}
		contexttest.LoadRepo(t, ctx, 1)
		prepareAscStackActions(ctx, pr)
		a, _ := ctx.Data["AscActions"].(*AscStackActions)
		return a
	}

	a := actions(2, f.top)
	require.NotNil(t, a)
	assert.True(t, a.CanQueue && a.CanMerge && a.CanRestack)
	assert.False(t, a.CanUnqueue || a.CanRevert)
	assert.Equal(t, "/user2/repo1/pulls/5/asc/confirm", a.ConfirmURL)
	assert.Equal(t, "/user2/repo1/asc/queue?branch=master", a.QueueURL)

	assert.Nil(t, actions(0, f.top), "a visitor gets no buttons")
	if a := actions(5, f.top); a != nil {
		assert.False(t, a.CanQueue || a.CanMerge || a.CanRestack || a.CanRevert || a.CanUnqueue, "a reader gets none")
	}

	// Queued: the button becomes "remove", and the state shows.
	_, err := automerge.AscQueueEnqueue(t.Context(), qrUser(t, 2), f.top)
	require.NoError(t, err)
	ctx, _ := contexttest.MockContext(t, "user2/repo1/pulls/5")
	contexttest.LoadUser(t, ctx, 2)
	ctx.IsSigned = true
	contexttest.LoadRepo(t, ctx, 1)
	prepareAscStackActions(ctx, f.top)
	a, _ = ctx.Data["AscActions"].(*AscStackActions)
	require.NotNil(t, a)
	assert.True(t, a.CanUnqueue)
	assert.False(t, a.CanQueue)
	assert.Equal(t, automerge.AscQueueQueued, a.QueueState)
	states, _ := ctx.Data["AscQueueStates"].(map[int64]string)
	assert.Equal(t, automerge.AscQueueQueued, states[f.top.Index])
	assert.Equal(t, automerge.AscQueueQueued, states[f.bottom.Index])

	// Merged: only the revert is offered.
	_, err = db.GetEngine(t.Context()).Exec("UPDATE `pull_request` SET has_merged = ? WHERE id = ?", true, f.top.ID)
	require.NoError(t, err)
	merged := unittest.AssertExistsAndLoadBean(t, &issues_model.PullRequest{ID: f.top.ID})
	a = actions(2, merged)
	require.NotNil(t, a)
	assert.True(t, a.CanRevert)
	assert.False(t, a.CanQueue || a.CanMerge || a.CanRestack)

	// A repository that is not in asc gets no buttons at all.
	defer automerge.UseAscQueueBacked(func(*repo_model.Repository) (bool, error) { return false, nil })()
	assert.Nil(t, actions(2, f.top))
}

func qrUser(t *testing.T, id int64) *user_model.User {
	t.Helper()
	return unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: id})
}

// Security: the browser relay never carries a write of the stack or the queue.
func TestAscStackMethodsAreNotRelayed(t *testing.T) {
	for _, name := range []string{
		"asc.v1.Stacks/Restack", "asc.v1.Stacks/RevertLanded", "asc.v1.Stacks/UndoRef",
		"asc.v1.MergeQueue/Enqueue", "asc.v1.MergeQueue/Dequeue", "asc.v1.MergeQueue/Revalidate", "asc.v1.MergeQueue/SetPaused",
	} {
		assert.False(t, ascRPCReadMethods[name], name)
	}
}
