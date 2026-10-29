// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package pull

import (
	"context"
	"time"

	issues_model "forgejo.org/models/issues"
	access_model "forgejo.org/models/perm/access"
	"forgejo.org/models/unit"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/ascclient"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/log"
	"forgejo.org/modules/setting"
	"forgejo.org/services/ascidentity"
)

// A review of a pull request that belongs to an asc stack is also the person's verdict on the change in asc: asc's policy
// (approvals on the exact version, the maintainer's human approval, no self-approval) is decided from what asc holds, so the hub
// tells it, as the reviewer: the call carries a one-use claim of that person (their immutable user id), never the hub's own
// identity. Only an approval that is not stale and a request for changes are reported. A stale approval, made on a head that has
// moved, is not claimed for the newest version.

const ascApprovalReportTimeout = 5 * time.Second

// ascApprovalReporter is nil until InitAscStack sets it. Tests replace it with UseAscApprovalReporter.
var ascApprovalReporter func(ctx context.Context, reviewer *user_model.User, repo string, change []byte, state ascv1.ApprovalState)

// UseAscApprovalReporter sets the reporter until the returned function is called. It is for tests.
func UseAscApprovalReporter(f func(ctx context.Context, reviewer *user_model.User, repo string, change []byte, state ascv1.ApprovalState)) (restore func()) {
	prev := ascApprovalReporter
	ascApprovalReporter = f
	return func() { ascApprovalReporter = prev }
}

// ascApprovalState is the state asc records for a review of this type, or false when nothing is reported.
func ascApprovalState(review *issues_model.Review) (ascv1.ApprovalState, bool) {
	if review == nil || review.Stale || review.Dismissed {
		return ascv1.ApprovalState_APPROVAL_STATE_UNSPECIFIED, false
	}
	switch review.Type {
	case issues_model.ReviewTypeApprove:
		return ascv1.ApprovalState_APPROVAL_STATE_APPROVED, true
	case issues_model.ReviewTypeReject:
		return ascv1.ApprovalState_APPROVAL_STATE_CHANGES_REQUESTED, true
	}
	return ascv1.ApprovalState_APPROVAL_STATE_UNSPECIFIED, false
}

// ascReportApproval tells asc about a review of a pull request of an asc stack. It never fails the review.
func ascReportApproval(ctx context.Context, doer *user_model.User, pr *issues_model.PullRequest, review *issues_model.Review) {
	if ascApprovalReporter == nil || doer == nil || pr == nil {
		return
	}
	state, ok := ascApprovalState(review)
	if !ok {
		return
	}
	name, raw, err := ascChangeOf(ctx, pr)
	if err != nil {
		return // not a stack, or not on asc
	}
	// Only a person with write access gives a verdict that asc counts; asc decides again from the claim.
	if pr.LoadBaseRepo(ctx) != nil {
		return
	}
	if perm, err := access_model.GetUserRepoPermission(ctx, pr.BaseRepo, doer); err != nil || !perm.CanWrite(unit.TypeCode) {
		return
	}
	ascApprovalReporter(ctx, doer, name, raw, state)
}

func installAscApprovalReporter() {
	if setting.InternalToken == "" {
		return
	}
	c, err := ascclient.New(ascclient.Config{Target: setting.Asc.ServerURL, InternalToken: setting.InternalToken, CallTimeout: ascApprovalReportTimeout})
	if err != nil {
		log.Error("asc approval reporter: %v", err)
		return
	}
	api := ascv1.NewReviewClient(c)
	ascApprovalReporter = func(ctx context.Context, reviewer *user_model.User, repo string, change []byte, state ascv1.ApprovalState) {
		uctx, err := ascidentity.AttachDoer(ascclient.WithSystem(context.WithoutCancel(ctx)), reviewer, ascidentity.RoleWrite, repo)
		if err != nil {
			log.Warn("asc approval: no claim for user %d: %v", reviewer.ID, err)
			return
		}
		cctx, cancel := context.WithTimeout(uctx, ascApprovalReportTimeout)
		defer cancel()
		if _, err := api.RecordApproval(cctx, &ascv1.RecordApprovalRequest{Repo: &ascv1.RepoRef{Name: repo}, Change: change, State: state}); err != nil {
			log.Warn("asc approval: RecordApproval for %s failed: %v", repo, err)
		}
	}
}
