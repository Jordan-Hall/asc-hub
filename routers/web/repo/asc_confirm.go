// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	"errors"
	"net/http"
	"strings"

	"forgejo.org/models"
	issues_model "forgejo.org/models/issues"
	"forgejo.org/services/automerge"
	"forgejo.org/services/context"
	pull_service "forgejo.org/services/pull"
)

// The confirmation page of a stack operation (merge when ready, remove from the queue, merge now, restack,
// revert). It runs the dry run of the operation, shows what it would meet and does nothing else; the form on it
// posts to the operation, which checks everything again. GET /{owner}/{repo}/pulls/{n}/asc/confirm?op=...

// AscConfirmRow is one pull request or change on the confirmation page.
type AscConfirmRow struct {
	Index  int64
	Title  string
	Link   string
	Status string // a suffix of the locale key repo.pulls.asc_op.state.<Status>
	Detail string // our own text: a reason or conflict paths
	Change string // short change id, when there is no pull request
}

// ascConfirmPR fills the pull request fields of row. The issue of pr must be loaded.
func ascConfirmPR(row *AscConfirmRow, pr *issues_model.PullRequest) {
	if pr == nil {
		return
	}
	if pr.Issue != nil {
		row.Index, row.Title, row.Link = pr.Issue.Index, pr.Issue.Title, pr.Issue.Link()
	} else {
		row.Index = pr.Index
	}
}

// ascCheckReason is the reason a pull request of a stack is not ready, in text this code or Forgejo wrote.
func ascCheckReason(err error) string {
	var dis models.ErrDisallowedToMerge
	if errors.As(err, &dis) && dis.Reason != "" {
		return dis.Reason
	}
	msg := err.Error()
	for _, s := range []error{pull_service.ErrAscStackNotMergable, pull_service.ErrAscStackNotAllowed, pull_service.ErrAscStackHeadMoved} {
		if rest, ok := strings.CutPrefix(msg, s.Error()+": "); ok {
			return rest
		}
	}
	return ""
}

func ascShortChange(c string) string {
	if len(c) > 12 {
		return c[:12]
	}
	return c
}

// ascConfirmFail records a dry run that failed: the message replaces the preview and the form is disabled. It
// reports true after it has written an answer (a server error or a 404).
func ascConfirmFail(ctx *context.Context, err error) bool {
	f := ascOpError(err)
	switch {
	case !f.Known:
		ctx.ServerError("asc confirm", err)
		return true
	case f.Status == http.StatusNotFound:
		ctx.NotFound("asc confirm", err)
		return true
	}
	ctx.Data["AscCanConfirm"] = false
	ctx.Data["AscFailure"] = ctx.Tr(f.Key, f.Args...)
	return false
}

// AscConfirm renders the dry run of an operation on the stack of a pull request.
func AscConfirm(ctx *context.Context) {
	issue, pr, ok := ascLandTarget(ctx)
	if !ok {
		return
	}
	op := ctx.FormString("op")
	base := issue.Link() + "/asc/"
	ctx.Data["AscOp"] = op
	ctx.Data["AscBack"] = issue.Link()
	ctx.Data["AscCanConfirm"] = true
	ctx.Data["AscPull"] = pr
	if op == ascOpMerge || op == ascOpQueue {
		// the scope of the land: the prefix of the stack that ends at the chosen pull request
		if panel, err := pull_service.BuildAscStackPanel(ctx, pr); err == nil {
			scopes := ascLandScopes(panel, issue.Index)
			ctx.Data["AscLandScopes"] = scopes
			ctx.Data["AscLandChoice"] = len(scopes) > 1
		}
		ctx.Data["AscLandTo"] = issue.Index
	}

	var written bool
	switch op {
	case ascOpQueue:
		ctx.Data["AscAction"] = base + "enqueue"
		written = ascConfirmMerge(ctx, pr, false)
	case ascOpMerge:
		ctx.Data["AscAction"] = base + "merge"
		written = ascConfirmMerge(ctx, pr, true)
	case ascOpUnqueue:
		ctx.Data["AscAction"] = base + "dequeue"
		written = ascConfirmUnqueue(ctx, pr)
	case ascOpRestack:
		ctx.Data["AscAction"] = base + "restack"
		written = ascConfirmRestack(ctx, pr)
	case ascOpRevert:
		ctx.Data["AscAction"] = base + "revert"
		written = ascConfirmRevert(ctx, pr)
	default:
		ctx.NotFound("asc confirm", nil)
		return
	}
	if written {
		return
	}
	ctx.Data["Title"] = ctx.Locale.TrString("repo.pulls.asc_op.title." + op)
	ctx.HTML(http.StatusOK, tplAscConfirm)
}

// ascConfirmMerge is the dry run of "Merge when ready" and "Merge now". Merge now needs every pull request to be
// ready and the branch not paused; merge when ready waits for what is not ready yet.
func ascConfirmMerge(ctx *context.Context, pr *issues_model.PullRequest, now bool) bool {
	d, err := automerge.AscQueueDryRunOf(ctx, ctx.Doer, pr)
	if err != nil {
		return ascConfirmFail(ctx, err)
	}
	var rows []AscConfirmRow
	for _, c := range d.Checks {
		row := AscConfirmRow{Status: "ready"}
		if err := ascLoadIssue(ctx, c.Item.PR); err != nil {
			ctx.ServerError("LoadIssue", err)
			return true
		}
		ascConfirmPR(&row, c.Item.PR)
		if e := d.Queued[c.Item.PR.ID]; c.Err != nil {
			row.Status, row.Detail = "not_ready", ascCheckReason(c.Err)
		} else if e != nil && e.State != automerge.AscQueueMerged && e.State != automerge.AscQueueCancelled {
			row.Status = "queued_" + e.State
		}
		rows = append(rows, row)
	}
	ctx.Data["AscRows"] = rows
	ctx.Data["AscPaused"] = d.Paused
	ctx.Data["AscNeedsContentMerge"] = d.NeedsContentMerge
	ctx.Data["AscNoContentMerge"] = d.NoContentMerge
	ctx.Data["AscReady"] = d.Ready
	if d.RestackErr != nil {
		if f := ascOpError(d.RestackErr); f.Known {
			ctx.Data["AscPreviewNote"] = ctx.Tr(f.Key, f.Args...)
		}
	}
	if now {
		ctx.Data["AscCanConfirm"] = d.Ready && !d.Paused
	}
	return false
}

// ascConfirmUnqueue shows the pull requests the removal takes out of the queue: the entries AscQueueCancel
// cancels, which are this pull request and the queued ones above it.
func ascConfirmUnqueue(ctx *context.Context, pr *issues_model.PullRequest) bool {
	set, err := automerge.AscQueueCancelSet(ctx, ctx.Doer, pr)
	if err != nil {
		return ascConfirmFail(ctx, err)
	}
	ctx.Data["AscSkippedCount"] = len(set.Skipped)
	var rows []AscConfirmRow
	for _, e := range set.Cancelled {
		queued, err := issues_model.GetPullRequestByID(ctx, e.PullID)
		if err != nil {
			continue
		}
		if err := ascLoadIssue(ctx, queued); err != nil {
			ctx.ServerError("LoadIssue", err)
			return true
		}
		row := AscConfirmRow{Status: "queued_" + e.State}
		ascConfirmPR(&row, queued)
		rows = append(rows, row)
	}
	ctx.Data["AscRows"] = rows
	ctx.Data["AscCanConfirm"] = len(rows) > 0
	return false
}

// ascConfirmRestack is asc's dry run of a restack of the stack.
func ascConfirmRestack(ctx *context.Context, pr *issues_model.PullRequest) bool {
	res, err := pull_service.RestackAscStack(ctx, ctx.Doer, pr, pull_service.AscRestackOptions{DryRun: true})
	if err != nil {
		return ascConfirmFail(ctx, err)
	}
	var rows []AscConfirmRow
	for _, it := range res.Items {
		row := AscConfirmRow{Status: string(it.State), Change: ascShortChange(it.Change), Detail: ascPathsArg(it.Conflicts)}
		if it.PR != nil {
			if err := ascLoadIssue(ctx, it.PR); err != nil {
				ctx.ServerError("LoadIssue", err)
				return true
			}
			ascConfirmPR(&row, it.PR)
		}
		rows = append(rows, row)
	}
	ctx.Data["AscRows"] = rows
	blocker := res.Blocker()
	ctx.Data["AscCanConfirm"] = blocker == nil && res.Needs()
	switch {
	case blocker != nil:
		f := ascOpError(blocker)
		ctx.Data["AscFailure"] = ctx.Tr(f.Key, f.Args...)
	case !res.Needs():
		ctx.Data["AscPreviewNote"] = ctx.Tr("repo.pulls.asc_op.restack_up_to_date")
	}
	return false
}

// ascConfirmRevert is asc's dry run of the revert of a landed stack, with the scope and squash of the form.
func ascConfirmRevert(ctx *context.Context, pr *issues_model.PullRequest) bool {
	this, ok := ascRevertScope(ctx)
	if !ok {
		ctx.Error(http.StatusBadRequest)
		return true
	}
	squash := ctx.FormBool("squash")
	opt := pull_service.AscRevertOptions{DryRun: true, Squash: squash}
	if this {
		opt.Bottom = pr
	}
	ctx.Data["AscScopeThis"] = this
	ctx.Data["AscSquash"] = squash
	res, err := pull_service.RevertAscStack(ctx, ctx.Doer, ctx.Repo.Permission, pr, opt)
	if err != nil {
		return ascConfirmFail(ctx, err)
	}
	var rows []AscConfirmRow
	for _, p := range res.Plans {
		for _, o := range p.Original {
			if err := ascLoadIssue(ctx, o); err != nil {
				ctx.ServerError("LoadIssue", err)
				return true
			}
			row := AscConfirmRow{Status: "revert"}
			ascConfirmPR(&row, o)
			if len(p.Conflicts) > 0 {
				row.Status, row.Detail = "conflict", ascPathsArg(p.Conflicts)
			}
			rows = append(rows, row)
		}
	}
	ctx.Data["AscRows"] = rows
	ctx.Data["AscCanConfirm"] = !res.HasConflicts()
	if res.HasConflicts() {
		ctx.Data["AscFailure"] = ctx.Tr("repo.pulls.asc_op.err.revert_conflict_preview")
	}
	return false
}
