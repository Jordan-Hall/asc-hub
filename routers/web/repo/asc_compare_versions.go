// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	gocontext "context"

	issues_model "forgejo.org/models/issues"
	"forgejo.org/modules/ascclient"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/log"
	"forgejo.org/services/asctrust"
	"forgejo.org/services/context"
)

// The header of the comparison of two versions of a stacked change: "+2 -1 in 1 of 3 files, and the approvals that
// still apply to the other two" (Review.CompareVersions). Advice only: without an answer the page has no summary.

// AscVersionCompare is what asc says about two versions.
type AscVersionCompare struct {
	From, To  uint32
	Changed   uint32
	Unchanged uint32
	Added     uint32
	Removed   uint32
	Total     uint32
	Truncated bool
	Approvals []AscCompareApproval
}

// AscCompareApproval is the verdict of one person on the older version, and whether it still stands for the newer one.
type AscCompareApproval struct {
	Reviewer         string
	ChangesRequested bool
	Applies          bool
	Carried          bool
	UnchangedFiles   uint32
}

// ascVersionCompareOf cleans the answer of asc.
func ascVersionCompareOf(from, to uint32, resp *ascv1.CompareVersionsResponse) *AscVersionCompare {
	out := &AscVersionCompare{
		From: from, To: to, Changed: resp.GetChanged(), Unchanged: resp.GetUnchanged(), Added: resp.GetAdded(), Removed: resp.GetRemoved(),
		Truncated: resp.GetTruncated(),
	}
	out.Total = out.Changed + out.Unchanged
	for i, a := range resp.GetApprovals() {
		if i >= ascBriefMaxApprovals {
			break
		}
		st := a.GetState()
		if st != ascv1.ApprovalState_APPROVAL_STATE_APPROVED && st != ascv1.ApprovalState_APPROVAL_STATE_CHANGES_REQUESTED {
			continue
		}
		out.Approvals = append(out.Approvals, AscCompareApproval{
			Reviewer: asctrust.CleanText(a.GetReviewer(), 80), ChangesRequested: st == ascv1.ApprovalState_APPROVAL_STATE_CHANGES_REQUESTED,
			Applies: a.GetApplies(), Carried: a.GetCarried(), UnchangedFiles: a.GetUnchangedFiles(),
		})
	}
	return out
}

// prepareAscVersionCompare puts the summary of the shown comparison into ctx.Data["AscCompare"]. It never fails the page.
func prepareAscVersionCompare(ctx *context.Context, pull *issues_model.PullRequest, view *ascVersionView) {
	if view == nil || view.from == 0 || view.to == 0 {
		return
	}
	link, err := issues_model.GetPullRequestAscStack(ctx, pull.ID)
	if err != nil || link == nil || pull.BaseRepo == nil && pull.LoadBaseRepo(ctx) != nil {
		return
	}
	name, err := ascGraphRepoName(pull.BaseRepo)
	change := ascChangeBytes(link.AscChange)
	if err != nil || change == nil {
		return
	}
	conn, err := ascRPCConn()
	if err != nil {
		return
	}
	callCtx, cancel := gocontext.WithTimeout(ascclient.WithSystem(ctx), ascBriefTimeout)
	defer cancel()
	resp, err := ascv1.NewReviewClient(conn).CompareVersions(callCtx, &ascv1.CompareVersionsRequest{
		Repo: &ascv1.RepoRef{Name: name}, Change: change, FromVersion: view.from, ToVersion: view.to,
	})
	if err != nil {
		log.Warn("asc compare versions: %v", err)
		return
	}
	ctx.Data["AscCompare"] = ascVersionCompareOf(view.from, view.to, resp)
}
