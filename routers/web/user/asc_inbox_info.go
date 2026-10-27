// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package user

import (
	gocontext "context"

	"forgejo.org/models/db"
	git_model "forgejo.org/models/git"
	issues_model "forgejo.org/models/issues"
	"forgejo.org/modules/gitrepo"
	"forgejo.org/modules/log"
	api "forgejo.org/modules/structs"
)

// What the dashboard rows and the inbox detail pane say about one pull request beyond its section: where it
// sits in its stack, how big it is, how its checks stand. Every value is read from the pull request the
// signed-in person can already see (the caller has checked read access through the inbox loader), and every
// failure leaves the value out instead of failing the page.

// ascStackWalkLimit bounds the walk up and down a stack, so a corrupt parent cycle cannot loop.
const ascStackWalkLimit = 64

// AscInboxInfo is the extra facts of one entry.
type AscInboxInfo struct {
	StackIndex int // 1 based position from the bottom, 0 when the pull request is not stacked
	StackTotal int

	HasDiff   bool
	Files     int
	Additions int
	Deletions int

	ChecksPass    int
	ChecksFail    int
	ChecksPending int

	Approvals int
}

// ascInboxInfoLoad returns the facts of the pull request of e. Tests replace it.
var ascInboxInfoLoad = ascInboxInfoFromModels

func ascInboxInfoFromModels(ctx gocontext.Context, e *AscInboxEntry) *AscInboxInfo {
	info := &AscInboxInfo{}
	info.StackIndex, info.StackTotal = ascStackPosition(ctx, e.PullID)

	pr, err := issues_model.GetPullRequestByID(ctx, e.PullID)
	if err != nil {
		return info
	}
	if err := pr.LoadBaseRepo(ctx); err != nil || pr.BaseRepo == nil {
		return info
	}
	reviewers, err := issues_model.GetAscInboxReviewers(ctx, pr.IssueID)
	if err == nil {
		for _, r := range reviewers {
			if r.Type == issues_model.ReviewTypeApprove && r.Official {
				info.Approvals++
			}
		}
	}
	if pr.HasMerged {
		return info
	}
	head := ascInboxHead(ctx, pr)
	if head == "" {
		return info
	}
	statuses, _, err := git_model.GetLatestCommitStatus(ctx, pr.BaseRepo.ID, head, db.ListOptionsAll)
	if err == nil {
		for _, s := range statuses {
			switch s.State {
			case api.CommitStatusSuccess, api.CommitStatusWarning:
				info.ChecksPass++
			case api.CommitStatusPending:
				info.ChecksPending++
			default:
				info.ChecksFail++
			}
		}
	}
	if pr.MergeBase == "" {
		return info
	}
	g, closer, err := gitrepo.RepositoryFromContextOrOpen(ctx, pr.BaseRepo)
	if err != nil {
		return info
	}
	defer closer.Close()
	files, adds, dels, err := g.GetShortStat(pr.MergeBase, head, false)
	if err != nil {
		log.Debug("asc inbox: no diff statistics for pull request %d: %v", pr.ID, err)
		return info
	}
	info.HasDiff, info.Files, info.Additions, info.Deletions = true, files, adds, dels
	return info
}

// ascStackPosition returns the 1 based place of the pull request pullID in its stack and the size of the
// stack, or 0, 0 when the pull request is not stacked.
func ascStackPosition(ctx gocontext.Context, pullID int64) (index, total int) {
	row, err := issues_model.GetPullRequestAscStack(ctx, pullID)
	if err != nil || row == nil {
		return 0, 0
	}
	index = 1
	root := row
	for i := 0; i < ascStackWalkLimit && root.StackParentID != 0; i++ {
		parent, err := issues_model.GetPullRequestAscStack(ctx, root.StackParentID)
		if err != nil || parent == nil {
			break
		}
		root = parent
		index++
	}
	return index, ascStackSize(ctx, root.PullID)
}

// ascStackSize counts the pull requests of the stack that starts at the pull request rootID.
func ascStackSize(ctx gocontext.Context, rootID int64) int {
	count := 0
	queue := []int64{rootID}
	seen := map[int64]bool{}
	for len(queue) > 0 && count < ascStackWalkLimit {
		id := queue[0]
		queue = queue[1:]
		if seen[id] {
			continue
		}
		seen[id] = true
		count++
		kids, err := issues_model.GetAscStackChildren(ctx, id)
		if err != nil {
			continue
		}
		for _, k := range kids {
			queue = append(queue, k.PullID)
		}
	}
	return count
}
