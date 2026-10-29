// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package pull

import (
	"context"
	"time"

	issues_model "forgejo.org/models/issues"
	repo_model "forgejo.org/models/repo"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/ascclient"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/log"
	"forgejo.org/modules/optional"
	"forgejo.org/modules/setting"
)

// A review of a pull request that belongs to an asc stack is also the person's verdict on the change in asc: asc's policy
// (approvals on the exact version, the maintainer's human approval, no self-approval) is decided from what asc holds, so the hub
// tells it, as the reviewer: the call carries a one-use claim of that person (their immutable user id), never the hub's own
// identity. The claim comes from the central gate asctrust.UserCall (write permission, account state, repository state), which
// services/asctrust installs with SetAscUserCall because it imports this package.
//
// Rules that hold for every report:
//   - A verdict is bound to the exact version that was reviewed: the commit of the review is looked up in the verified versions of
//     the change and the report carries that number. A review whose commit matches no verified version is not reported; asc is
//     never asked to guess "the newest".
//   - Only an approval that is not stale and a request for changes are reported as a verdict.
//   - A dismissal (by a maintainer, or automatically because commits were pushed) is reported as DISMISSED naming the reviewer by
//     the hub's user id, the caller being the person who dismissed. It is reported when the reviewer has no other standing
//     verdict on the pull request. Un-dismissing is not reported: that would have the hub speak for a person who did not act, so
//     the verdict stays dismissed in asc until the reviewer reviews again (fail closed).
//   - Nothing here fails the review or the dismissal; a report that cannot be made is logged.

const ascApprovalReportTimeout = 5 * time.Second

// AscApprovalReport is one verdict, or one dismissal, for asc.
type AscApprovalReport struct {
	Repo    string
	Change  []byte
	Version uint32 // the exact version the verdict is about; never 0
	State   ascv1.ApprovalState
	// ReviewerID is the hub id of the person whose verdicts are dismissed. It is 0 for the caller's own verdict.
	ReviewerID int64
}

var (
	// ascApprovalReporter is nil until InitAscStack sets it. callCtx carries the claim of doer. Tests replace it with
	// UseAscApprovalReporter.
	ascApprovalReporter func(callCtx context.Context, doer *user_model.User, r AscApprovalReport)

	// ascUserCall gives the context of a call to asc made for doer on repo, or ok false when the hub does not allow it to the
	// person. It is nil until services/asctrust installs asctrust.UserCall; while it is nil nothing is reported.
	ascUserCall func(ctx context.Context, doer *user_model.User, repo *repo_model.Repository) (callCtx context.Context, ok bool, err error)
)

// SetAscUserCall installs the gate of a person's write call to asc. It returns a function that puts the previous one back.
func SetAscUserCall(f func(ctx context.Context, doer *user_model.User, repo *repo_model.Repository) (context.Context, bool, error)) (restore func()) {
	prev := ascUserCall
	ascUserCall = f
	return func() { ascUserCall = prev }
}

// UseAscApprovalReporter sets the reporter until the returned function is called. It is for tests.
func UseAscApprovalReporter(f func(callCtx context.Context, doer *user_model.User, r AscApprovalReport)) (restore func()) {
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

// ascApprovalTarget returns the change of pr and the call context of doer, or false when nothing is to be reported.
func ascApprovalTarget(ctx context.Context, doer *user_model.User, pr *issues_model.PullRequest) (callCtx context.Context, name string, raw []byte, ok bool) {
	if ascApprovalReporter == nil || ascUserCall == nil || doer == nil || pr == nil {
		return nil, "", nil, false
	}
	name, raw, err := ascChangeOf(ctx, pr)
	if err != nil {
		return nil, "", nil, false // not a stack, or not on asc
	}
	if err := pr.LoadBaseRepo(ctx); err != nil {
		return nil, "", nil, false
	}
	// Only a person the hub lets write gives a verdict that asc counts; asc decides again from the claim.
	callCtx, allowed, err := ascUserCall(ctx, doer, pr.BaseRepo)
	if err != nil {
		log.Warn("asc approval: no call for user %d: %v", doer.ID, err)
		return nil, "", nil, false
	}
	if !allowed {
		return nil, "", nil, false
	}
	return callCtx, name, raw, true
}

// ascReportApproval tells asc about a review of a pull request of an asc stack, bound to the version the review was made on. It
// never fails the review.
func ascReportApproval(ctx context.Context, doer *user_model.User, pr *issues_model.PullRequest, review *issues_model.Review) {
	state, ok := ascApprovalState(review)
	if !ok {
		return
	}
	callCtx, name, raw, ok := ascApprovalTarget(ctx, doer, pr)
	if !ok {
		return
	}
	versions, err := AscVersions(ctx, pr)
	if err != nil {
		log.Warn("asc approval: the versions of pull request %d: %v", pr.ID, err)
		return
	}
	version := versions.ByGitCommit(review.CommitID)
	if version == 0 {
		log.Warn("asc approval: commit %q of the review is no verified version of pull request %d; not reported", review.CommitID, pr.ID)
		return
	}
	ascApprovalReporter(callCtx, doer, AscApprovalReport{Repo: name, Change: raw, Version: version, State: state})
}

// ascReportDismissal tells asc that dismissed, a review that doer has just dismissed, no longer counts. If the reviewer still has
// another standing verdict on the pull request nothing is reported: that verdict was reported when it was made. It never fails the
// dismissal.
func ascReportDismissal(ctx context.Context, doer *user_model.User, pr *issues_model.PullRequest, dismissed *issues_model.Review) {
	if dismissed == nil || dismissed.ReviewerID <= 0 || (dismissed.Type != issues_model.ReviewTypeApprove && dismissed.Type != issues_model.ReviewTypeReject) {
		return
	}
	standing, err := issues_model.FindReviews(ctx, issues_model.FindReviewOptions{
		IssueID:    dismissed.IssueID,
		ReviewerID: dismissed.ReviewerID,
		Types:      []issues_model.ReviewType{issues_model.ReviewTypeApprove, issues_model.ReviewTypeReject},
		Dismissed:  optional.Some(false),
	})
	if err != nil || len(standing) > 0 {
		return
	}
	callCtx, name, raw, ok := ascApprovalTarget(ctx, doer, pr)
	if !ok {
		return
	}
	versions, err := AscVersions(ctx, pr)
	if err != nil {
		log.Warn("asc approval: the versions of pull request %d: %v", pr.ID, err)
		return
	}
	// A dismissal only ever takes verdicts away, so a commit that is no version falls back to the newest one.
	version := versions.ByGitCommit(dismissed.CommitID)
	if version == 0 {
		version = versions.Latest()
	}
	if version == 0 {
		return
	}
	ascApprovalReporter(callCtx, doer, AscApprovalReport{Repo: name, Change: raw, Version: version, State: ascv1.ApprovalState_APPROVAL_STATE_DISMISSED, ReviewerID: dismissed.ReviewerID})
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
	ascApprovalReporter = func(callCtx context.Context, _ *user_model.User, r AscApprovalReport) {
		cctx, cancel := context.WithTimeout(context.WithoutCancel(callCtx), ascApprovalReportTimeout)
		defer cancel()
		req := &ascv1.RecordApprovalRequest{Repo: &ascv1.RepoRef{Name: r.Repo}, Change: r.Change, Version: r.Version, State: r.State}
		if r.ReviewerID > 0 {
			req.ReviewerId = uint64(r.ReviewerID)
		}
		if _, err := api.RecordApproval(cctx, req); err != nil {
			log.Warn("asc approval: RecordApproval for %s failed: %v", r.Repo, err)
		}
	}
}
