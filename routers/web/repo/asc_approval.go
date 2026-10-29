// Copyright 2026 The ASC Hub Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	gocontext "context"

	issues_model "forgejo.org/models/issues"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/gitrepo"
	"forgejo.org/modules/log"
	"forgejo.org/modules/setting"
	"forgejo.org/services/asctrust"
	"forgejo.org/services/context"
)

// The verdicts of the hub's review form are also the verdicts asc counts for the gate and for "approvals carried over"
// from one version to the next. The hub reports each of them with Review.RecordApproval under the credential of the
// person who gave it, so asc names the reviewer from the forwarded claim (the immutable ids of the person) and never
// from a name in the request. A failure is logged and never undoes the review: the hub's review is the record, asc's
// copy is rebuilt by the next verdict.

// ascApprovalState maps the type of a hub review to the state asc keeps, or false for a review that is no verdict.
func ascApprovalState(t issues_model.ReviewType) (ascv1.ApprovalState, bool) {
	switch t {
	case issues_model.ReviewTypeApprove:
		return ascv1.ApprovalState_APPROVAL_STATE_APPROVED, true
	case issues_model.ReviewTypeReject:
		return ascv1.ApprovalState_APPROVAL_STATE_CHANGES_REQUESTED, true
	}
	return 0, false
}

// reportAscApproval reports the verdict of ctx.Doer on a stacked pull request to asc.
func reportAscApproval(ctx *context.Context, issue *issues_model.Issue, reviewType issues_model.ReviewType) {
	state, ok := ascApprovalState(reviewType)
	if !ok || !setting.Asc.Enabled || ctx.Doer == nil || issue == nil || !issue.IsPull {
		return
	}
	if err := issue.LoadPullRequest(ctx); err != nil {
		return
	}
	pull := issue.PullRequest
	link, err := issues_model.GetPullRequestAscStack(ctx, pull.ID)
	if err != nil || link == nil {
		return
	}
	if pull.BaseRepo == nil && pull.LoadBaseRepo(ctx) != nil {
		return
	}
	repo := pull.BaseRepo
	if repo.GetStorageBackend() != string(gitrepo.StorageBackendAsc) {
		return
	}
	name, err := asctrust.RepoName(repo)
	if err != nil {
		return
	}
	change := ascChangeBytes(link.AscChange)
	if change == nil {
		return
	}
	conn, err := ascRPCConn()
	if err != nil {
		return
	}
	userCtx, release, err := asctrust.UserCall(ctx, ctx.Doer, asctrust.CallRepoWrite, repo)
	if err != nil {
		log.Warn("asc approval: credential of user %d: %v", ctx.Doer.ID, err)
		return
	}
	defer release()
	callCtx, cancel := gocontext.WithTimeout(userCtx, ascBriefTimeout)
	defer cancel()
	if _, err := ascv1.NewReviewClient(conn).RecordApproval(callCtx, &ascv1.RecordApprovalRequest{
		Repo: &ascv1.RepoRef{Name: name}, Change: change, State: state,
	}); err != nil {
		log.Warn("asc approval: pull request %d: %v", pull.ID, err)
	}
}
