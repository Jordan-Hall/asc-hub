// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	"errors"
	"net/http"
	"net/url"
	"strings"
	"unicode/utf8"

	issues_model "forgejo.org/models/issues"
	"forgejo.org/models/unit"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/log"
	"forgejo.org/modules/setting"
	"forgejo.org/services/ascbridge"
	"forgejo.org/services/automerge"
	"forgejo.org/services/context"
	pull_service "forgejo.org/services/pull"
)

// The merge queue and the stack operations on the web (docs/BUILDOUT.md sections 6 and 7):
//
//	GET  /{owner}/{repo}/asc/queue                      the queue of a branch (templates/repo/asc_queue.tmpl)
//	POST /{owner}/{repo}/asc/queue/pause|resume         pause or resume the branch (administrators)
//	GET  /{owner}/{repo}/pulls/{n}/asc/confirm?op=...   the dry run and the confirmation of an operation
//	POST /{owner}/{repo}/pulls/{n}/asc/enqueue          merge when ready, for the stack from the bottom up to n
//	POST /{owner}/{repo}/pulls/{n}/asc/dequeue          take n (and what is queued above it) out of the queue
//	POST /{owner}/{repo}/pulls/{n}/asc/merge            merge the stack from the bottom up to n now
//	POST /{owner}/{repo}/pulls/{n}/asc/restack          restack the stack of n onto the target
//	POST /{owner}/{repo}/pulls/{n}/asc/revert           revert the landed stack of n
//
// Every write is a POST behind sign-in and the CSRF check of the router. The handlers answer 404 when asc is
// off or the viewer cannot read the pull requests, 403 for a visitor who is not signed in, and leave the decision
// whether the doer may do the operation to the service, which fails closed. Operations are reached from the
// confirmation page, which runs the dry run first, so nothing is written by a link.

// The operations of the confirmation page.
const (
	ascOpQueue   = "queue"
	ascOpUnqueue = "unqueue"
	ascOpMerge   = "merge"
	ascOpRestack = "restack"
	ascOpRevert  = "revert"
)

const (
	tplAscQueue   = "repo/asc_queue"
	tplAscConfirm = "repo/asc_confirm"

	// ascMaxBranchBytes bounds the branch name taken from a request.
	ascMaxBranchBytes = 255
)

// ascStackTarget loads the pull request of an asc stack operation after the checks every one of them shares.
// It reports false after it has answered.
func ascStackTarget(ctx *context.Context) (*issues_model.Issue, *issues_model.PullRequest, bool) {
	if !ctx.IsSigned {
		ctx.Error(http.StatusForbidden)
		return nil, nil, false
	}
	if !setting.Asc.Enabled || !ctx.Repo.CanRead(unit.TypePullRequests) {
		ctx.NotFound("asc stack operation", nil)
		return nil, nil, false
	}
	issue, ok := getPullInfo(ctx)
	if !ok {
		return nil, nil, false
	}
	return issue, issue.PullRequest, true
}

// ascValidBranch reports whether name can be a branch name taken from a request: not empty, bounded, valid
// UTF-8, no control character.
func ascValidBranch(name string) bool {
	if name == "" || len(name) > ascMaxBranchBytes || !utf8.ValidString(name) {
		return false
	}
	for _, r := range name {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}

// ascOpFailure is how an error of an operation is shown.
type ascOpFailure struct {
	Key    string // locale key
	Args   []any
	Status int // the status of a page answer; flashes redirect and use 0
	Known  bool
}

// ascMaxShownPaths bounds the conflict paths named in a message.
const ascMaxShownPaths = 5

func ascPathsArg(paths []string) string {
	if len(paths) > ascMaxShownPaths {
		paths = append(paths[:ascMaxShownPaths:ascMaxShownPaths], "…")
	}
	return strings.Join(paths, ", ")
}

// ascOpError maps an error of a stack operation to its message. Only text this code wrote is shown: no error of
// asc or of a database reaches the page. known is false for an error the caller must treat as a server error.
func ascOpError(err error) ascOpFailure {
	var conflict *ascbridge.ConflictError
	switch {
	case err == nil:
		return ascOpFailure{}
	case errors.Is(err, pull_service.ErrAscStackNotStacked):
		return ascOpFailure{Key: "repo.pulls.asc_op.err.not_stacked", Status: http.StatusNotFound, Known: true}
	case errors.As(err, &conflict):
		key := "repo.pulls.asc_op.err.conflict"
		if errors.Is(err, ascbridge.ErrRevertConflict) {
			key = "repo.pulls.asc_op.err.revert_conflict"
		}
		return ascOpFailure{Key: key, Args: []any{ascPathsArg(conflict.Paths)}, Known: true}
	}
	for _, e := range []struct {
		target error
		key    string
		status int
	}{
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
	} {
		if errors.Is(err, e.target) {
			return ascOpFailure{Key: "repo.pulls.asc_op.err." + e.key, Status: e.status, Known: true}
		}
	}
	// Not ready and not queueable carry our own text (a pull request number and a reason).
	if errors.Is(err, pull_service.ErrAscStackNotMergable) || errors.Is(err, automerge.ErrAscQueueNotQueueable) {
		return ascOpFailure{Key: "repo.pulls.asc_op.err.not_ready", Args: []any{err.Error()}, Known: true}
	}
	return ascOpFailure{}
}

// ascOpFlashFail answers a failed operation: a flash on the pull request page, a 403 for a refused doer, a 404
// for a missing stack, a server error for what is not known.
func ascOpFlashFail(ctx *context.Context, op string, issue *issues_model.Issue, err error) {
	f := ascOpError(err)
	switch {
	case !f.Known:
		ctx.ServerError("asc "+op, err)
	case f.Status == http.StatusNotFound:
		ctx.NotFound("asc "+op, err)
	default:
		if f.Status == http.StatusForbidden {
			log.Warn("asc %s refused for user %d on pull request %d: %v", op, ctx.Doer.ID, issue.ID, err)
		}
		ctx.Flash.Error(ctx.Tr(f.Key, f.Args...))
		ctx.Redirect(issue.Link())
	}
}

// AscQueueEnqueue handles "Merge when ready" for the stack from the bottom up to this pull request.
func AscQueueEnqueue(ctx *context.Context) {
	issue, pr, ok := ascStackTarget(ctx)
	if !ok {
		return
	}
	res, err := automerge.AscQueueEnqueue(ctx, ctx.Doer, pr)
	if err != nil {
		ascOpFlashFail(ctx, ascOpQueue, issue, err)
		return
	}
	ctx.Flash.Success(ctx.Tr("repo.pulls.asc_op.queued", len(res.Queued)))
	ctx.Redirect(issue.Link())
}

// AscQueueDequeue takes this pull request, and the queued pull requests above it in the stack, out of the queue.
func AscQueueDequeue(ctx *context.Context) {
	issue, pr, ok := ascStackTarget(ctx)
	if !ok {
		return
	}
	if err := automerge.AscQueueCancel(ctx, ctx.Doer, pr); err != nil {
		ascOpFlashFail(ctx, ascOpUnqueue, issue, err)
		return
	}
	ctx.Flash.Success(ctx.Tr("repo.pulls.asc_op.dequeued"))
	ctx.Redirect(issue.Link())
}

// AscQueueMergeNow merges the stack from the bottom up to this pull request at once, under the lease of the
// branch so no other land runs beside it.
func AscQueueMergeNow(ctx *context.Context) {
	issue, pr, ok := ascStackTarget(ctx)
	if !ok {
		return
	}
	merged, err := automerge.AscQueueLandNow(ctx, ctx.Doer, pr)
	if err != nil {
		ascOpFlashFail(ctx, ascOpMerge, issue, err)
		return
	}
	ctx.Flash.Success(ctx.Tr("repo.pulls.asc_op.merged", len(merged)))
	ctx.Redirect(issue.Link())
}

// AscQueuePage renders the queue of one branch (the default branch unless ?branch= says otherwise).
func AscQueuePage(ctx *context.Context) {
	if !setting.Asc.Enabled || !ctx.Repo.CanRead(unit.TypePullRequests) {
		ctx.NotFound("asc queue", nil)
		return
	}
	repo := ctx.Repo.Repository
	if ok, err := automerge.AscQueueBackedBy(repo); err != nil {
		ctx.ServerError("AscQueueBackedBy", err)
		return
	} else if !ok {
		ctx.NotFound("asc queue", nil)
		return
	}
	branch := strings.TrimSpace(ctx.FormString("branch"))
	if branch == "" {
		branch = repo.DefaultBranch
	}
	if !ascValidBranch(branch) {
		ctx.NotFound("asc queue", nil)
		return
	}
	view, err := automerge.AscQueueViewOf(ctx, repo, branch, ctx.Doer)
	if err != nil {
		ctx.ServerError("AscQueueViewOf", err)
		return
	}
	canAdmin, err := automerge.AscQueueMayAdmin(ctx, ctx.Doer, repo)
	if err != nil {
		ctx.ServerError("AscQueueMayAdmin", err)
		return
	}
	var ids []int64
	for _, rows := range [][]automerge.AscQueueRow{view.Active, view.Recent} {
		for _, r := range rows {
			ids = append(ids, r.Entry.DoerID)
		}
	}
	list, err := user_model.GetUsersByIDs(ctx, ids)
	if err != nil {
		ctx.ServerError("GetUsersByIDs", err)
		return
	}
	users := make(map[int64]*user_model.User, len(list))
	for _, u := range list {
		users[u.ID] = u
	}
	ctx.Data["Title"] = ctx.Locale.TrString("repo.asc_queue.title")
	ctx.Data["PageIsAscQueue"] = true
	ctx.Data["AscQueue"] = view
	ctx.Data["AscQueueUsers"] = users
	ctx.Data["AscQueueCanAdmin"] = canAdmin
	ctx.Data["AscQueueBranch"] = branch
	ctx.Data["AscQueueRevalidate"] = automerge.AscQueueNoContentMerge()
	ctx.HTML(http.StatusOK, tplAscQueue)
}

// ascQueueSetPaused is the shared body of AscQueuePause and AscQueueResume.
func ascQueueSetPaused(ctx *context.Context, paused bool) {
	if !ctx.IsSigned {
		ctx.Error(http.StatusForbidden)
		return
	}
	if !setting.Asc.Enabled || !ctx.Repo.CanRead(unit.TypePullRequests) {
		ctx.NotFound("asc queue", nil)
		return
	}
	branch := strings.TrimSpace(ctx.FormString("branch"))
	if !ascValidBranch(branch) {
		ctx.NotFound("asc queue", nil)
		return
	}
	back := ctx.Repo.RepoLink + "/asc/queue?branch=" + url.QueryEscape(branch)
	err := automerge.AscQueueSetPaused(ctx, ctx.Doer, ctx.Repo.Repository, branch, paused)
	switch {
	case errors.Is(err, automerge.ErrAscQueueNoPermission):
		ctx.Error(http.StatusForbidden)
		return
	case err != nil:
		ctx.ServerError("AscQueueSetPaused", err)
		return
	}
	if paused {
		ctx.Flash.Success(ctx.Tr("repo.asc_queue.paused_done"))
	} else {
		ctx.Flash.Success(ctx.Tr("repo.asc_queue.resumed_done"))
		automerge.AscQueueKick(ctx.Repo.Repository.ID, branch)
	}
	ctx.Redirect(back)
}

// AscQueuePause pauses the queue of a branch. Only an administrator may.
func AscQueuePause(ctx *context.Context) { ascQueueSetPaused(ctx, true) }

// AscQueueResume resumes the queue of a branch and asks for a run. Only an administrator may.
func AscQueueResume(ctx *context.Context) { ascQueueSetPaused(ctx, false) }
