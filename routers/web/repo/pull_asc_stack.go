// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	issues_model "forgejo.org/models/issues"
	"forgejo.org/modules/log"
	"forgejo.org/modules/optional"
	"forgejo.org/services/ascinbox"
	"forgejo.org/services/context"
	pull_service "forgejo.org/services/pull"
)

// The stack panel and the mid-stack merge block of the pull request page (SPEC 20.5, PLAN unit C36). The
// template is templates/repo/pulls/asc_stack.tmpl.

// prepareAscStackPanel puts the stack panel of a stacked pull request into ctx.Data. It does nothing for a
// pull request that is not stacked, and reports false after it has answered with an error.
func prepareAscStackPanel(ctx *context.Context, pull *issues_model.PullRequest) bool {
	if pull != nil && pull.BaseRepo == nil {
		_ = pull.LoadBaseRepo(ctx)
	}
	panel, err := pull_service.BuildAscStackPanel(ctx, pull)
	if err != nil {
		ctx.ServerError("BuildAscStackPanel", err)
		return false
	}
	if panel != nil {
		enrichAscStackPanel(ctx, panel)
		prepareAscRestackLine(ctx, pull)
		ctx.Data["AscStackPanel"] = panel
		prepareAscStackActions(ctx, pull)
	}
	return true
}

// ascStackReview is the review state a rail row names: changes (a person asked for changes), approved, draft, or "".
func ascStackReview(draft, rejected, approved bool) string {
	switch {
	case rejected:
		return "changes"
	case approved:
		return "approved"
	case draft:
		return "draft"
	}
	return ""
}

// enrichAscStackPanel gives each row of the rail its author, its size and its review state. Each part is advice: a row
// whose pull request or diff cannot be read stays as it is.
func enrichAscStackPanel(ctx *context.Context, panel *pull_service.AscStackPanel) {
	if panel == nil || ctx.Repo == nil || ctx.Repo.Repository == nil {
		return
	}
	repo := ctx.Repo.Repository
	for i := range panel.Entries {
		e := &panel.Entries[i]
		pr, err := issues_model.GetPullRequestByIndex(ctx, repo.ID, e.Index)
		if err != nil {
			continue
		}
		pr.BaseRepo = repo
		if err := pr.LoadIssue(ctx); err != nil {
			continue
		}
		if err := pr.Issue.LoadPoster(ctx); err == nil && pr.Issue.Poster != nil {
			e.Poster = pr.Issue.Poster
		}
		if e.State == pull_service.AscStackStateOpen {
			if ctx.Repo.GitRepo != nil {
				if sha := ascinbox.HeadCommit(ctx, pr); sha != "" {
					base := pr.MergeBase
					if base == "" {
						base = pr.BaseBranch
					}
					if _, add, del, err := ctx.Repo.GitRepo.GetShortStat(base, sha, base == pr.BaseBranch); err == nil {
						e.Additions, e.Deletions, e.HasSize = add, del, true
					}
				}
			}
			reviews, err := issues_model.FindLatestReviews(ctx, issues_model.FindReviewOptions{
				IssueID: pr.IssueID, Dismissed: optional.Some(false),
				Types: []issues_model.ReviewType{issues_model.ReviewTypeApprove, issues_model.ReviewTypeReject},
			})
			if err != nil {
				log.Warn("asc stack rail: reviews of pull request %d: %v", pr.ID, err)
			}
			rejected, approved := false, false
			for _, r := range reviews {
				rejected = rejected || r.Type == issues_model.ReviewTypeReject
				approved = approved || r.Type == issues_model.ReviewTypeApprove
			}
			e.Review = ascStackReview(pr.IsWorkInProgress(ctx), rejected, approved)
		}
	}
}

// ascStackMergeBlocked answers a web merge of a pull request whose parent has not merged with the JSON
// error of the merge form, and reports true when it did. A lookup error also stops the merge.
func ascStackMergeBlocked(ctx *context.Context, pr *issues_model.PullRequest) bool {
	parent, err := pull_service.AscStackParentBlocking(ctx, pr)
	if err != nil {
		ctx.ServerError("AscStackParentBlocking", err)
		return true
	}
	if parent == nil {
		return false
	}
	ctx.JSONError(ctx.Tr("repo.pulls.asc_stack_parent_unmerged"))
	return true
}
