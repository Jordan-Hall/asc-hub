// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	"net/http"
	"strconv"

	issues_model "forgejo.org/models/issues"
	access_model "forgejo.org/models/perm/access"
	"forgejo.org/models/unit"
	"forgejo.org/modules/gitrepo"
	"forgejo.org/modules/log"
	"forgejo.org/modules/setting"
	"forgejo.org/services/automerge"
	"forgejo.org/services/context"
	pull_service "forgejo.org/services/pull"
)

// AscDashAct serves `POST /-/asc/dash/act`: a row of "Needs you" on the dashboard acts in place. The form names the
// pull request (pull, its id) and what to do:
//
//   - approve: an approving review at the head of the pull request, as Approve on the review page. The form names
//     the head the row showed (head); a pull request pushed to since is not approved, so nobody approves code unseen;
//   - queue: the stack up to the pull request into the asc merge queue, as "Merge when ready";
//   - unqueue: the undo of queue, the stack out of the queue again.
//
// A pull request the person may not read is a 404, so the id tells nothing. The answer goes back to the dashboard: a
// flash for approve and unqueue and for a refusal, ?queued= for queue so the dashboard shows the toast that can undo.
func AscDashAct(ctx *context.Context) {
	if !setting.Asc.Enabled || ctx.Doer == nil {
		ctx.NotFound("AscDashAct", nil)
		return
	}
	pullID, err := strconv.ParseInt(ctx.FormString("pull"), 10, 64)
	if err != nil || pullID <= 0 {
		ctx.Error(http.StatusBadRequest)
		return
	}
	do := ctx.FormString("do")
	if do != "approve" && do != "queue" && do != "unqueue" {
		ctx.Error(http.StatusBadRequest)
		return
	}
	pr, err := issues_model.GetPullRequestByID(ctx, pullID)
	if err != nil {
		if issues_model.IsErrPullRequestNotExist(err) {
			ctx.NotFound("AscDashAct", nil)
		} else {
			ctx.ServerError("GetPullRequestByID", err)
		}
		return
	}
	if err := pr.LoadBaseRepo(ctx); err != nil {
		ctx.ServerError("LoadBaseRepo", err)
		return
	}
	perm, err := access_model.GetUserRepoPermission(ctx, pr.BaseRepo, ctx.Doer)
	if err != nil {
		ctx.ServerError("GetUserRepoPermission", err)
		return
	}
	if !perm.CanRead(unit.TypePullRequests) {
		ctx.NotFound("AscDashAct", nil)
		return
	}
	if err := pr.LoadIssue(ctx); err != nil {
		ctx.ServerError("LoadIssue", err)
		return
	}
	pr.Issue.PullRequest = pr
	pr.Issue.Repo = pr.BaseRepo
	back := setting.AppSubURL + "/"
	if pr.BaseRepo.IsArchived {
		ctx.Flash.Error(ctx.Tr("user.asc_dash.act_archived"))
		ctx.Redirect(back)
		return
	}
	switch do {
	case "approve":
		ascDashApprove(ctx, pr, back)
	case "queue":
		if _, err := automerge.AscQueueEnqueue(ctx, ctx.Doer, pr); err != nil {
			ascDashOpFail(ctx, ascOpQueue, pr, back, err)
			return
		}
		ctx.Redirect(back + "?queued=" + strconv.FormatInt(pr.ID, 10))
	case "unqueue":
		res, err := automerge.AscQueueCancelEntries(ctx, ctx.Doer, pr)
		if err != nil {
			ascDashOpFail(ctx, ascOpUnqueue, pr, back, err)
			return
		}
		ctx.Flash.Success(ctx.Tr("repo.pulls.asc_op.dequeued"))
		if len(res.Skipped) > 0 {
			ctx.Flash.Warning(ctx.Tr("repo.pulls.asc_op.dequeued_others", len(res.Skipped)))
		}
		ctx.Redirect(back)
	}
}

// ascDashApprove approves pr at its head commit for the person, who may not approve their own pull request.
func ascDashApprove(ctx *context.Context, pr *issues_model.PullRequest, back string) {
	issue := pr.Issue
	switch {
	case issue.IsClosed || pr.HasMerged:
		ctx.Flash.Error(ctx.Tr("user.asc_dash.act_closed"))
		ctx.Redirect(back)
		return
	case issue.IsPoster(ctx.Doer.ID):
		ctx.Flash.Error(ctx.Tr("repo.issues.review.self.approval"))
		ctx.Redirect(back)
		return
	}
	gitRepo, err := gitrepo.OpenRepository(ctx, pr.BaseRepo)
	if err != nil {
		ctx.ServerError("OpenRepository", err)
		return
	}
	defer gitRepo.Close()
	head, err := gitRepo.GetRefCommitID(pr.GetGitRefName())
	if err != nil {
		ctx.ServerError("GetRefCommitID", err)
		return
	}
	if seen := ctx.FormString("head"); seen == "" || seen != head {
		ctx.Flash.Error(ctx.Tr("user.asc_dash.act_stale"))
		ctx.Redirect(back)
		return
	}
	if _, _, err := pull_service.SubmitReview(ctx, ctx.Doer, gitRepo, issue, issues_model.ReviewTypeApprove, "", head, nil); err != nil {
		ctx.ServerError("SubmitReview", err)
		return
	}
	ctx.Flash.Success(ctx.Tr("user.asc_dash.approved_flash", pr.BaseRepo.FullName()+"#"+strconv.FormatInt(issue.Index, 10)))
	ctx.Redirect(back)
}

// ascDashOpFail answers a refused queue operation of a dashboard row with its message on the dashboard. Only text
// this code wrote is shown (ascOpError); what it does not know is a server error.
func ascDashOpFail(ctx *context.Context, op string, pr *issues_model.PullRequest, back string, err error) {
	f := ascOpError(err)
	if !f.Known {
		ctx.ServerError("asc "+op, err)
		return
	}
	if f.Status == http.StatusForbidden {
		log.Warn("asc %s refused for user %d on pull request %d: %v", op, ctx.Doer.ID, pr.ID, err)
	}
	ctx.Flash.Error(ctx.Tr(f.Key, f.Args...))
	ctx.Redirect(back)
}
