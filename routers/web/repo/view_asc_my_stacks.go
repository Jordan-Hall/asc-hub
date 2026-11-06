// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	issues_model "forgejo.org/models/issues"
	"forgejo.org/models/unit"
	"forgejo.org/modules/gitrepo"
	"forgejo.org/modules/log"
	"forgejo.org/modules/util"
	"forgejo.org/services/context"
)

// The Propose change bar of an asc repository's file view (design board Code) offers "Start a new change" and "Add to
// my stack". Adding to a stack edits the file on the head of one of the person's open stacks onto the shown branch,
// so the commit becomes the next change of that stack.

// ascMyStacksMax bounds the stacks offered in the bar.
const ascMyStacksMax = 10

// AscMyStack is one open stack of the person that the bar can add the edit to.
type AscMyStack struct {
	Title string
	// EditLink opens the editor on the stack's head for the shown file.
	EditLink string
}

// ascMyStacks keeps the open pull requests of doerID from the repository itself, newest first, at most
// ascMyStacksMax. A pull request from a fork cannot take a commit from this repository's editor.
func ascMyStacks(prs []*issues_model.PullRequest, repoID, doerID int64, editLink func(branch string) string) []AscMyStack {
	out := []AscMyStack{}
	for _, pr := range prs {
		if len(out) >= ascMyStacksMax {
			break
		}
		if pr.Issue == nil || pr.Issue.PosterID != doerID || pr.HeadRepoID != repoID || pr.HeadBranch == "" {
			continue
		}
		out = append(out, AscMyStack{Title: pr.Issue.Title, EditLink: editLink(pr.HeadBranch)})
	}
	return out
}

// prepareAscMyStacks puts the person's open stacks onto the shown branch into ctx.Data for the Propose change bar.
func prepareAscMyStacks(ctx *context.Context) {
	repo := ctx.Repo.Repository
	if ctx.Doer == nil || !ctx.Repo.IsViewBranch || ctx.Repo.TreePath == "" ||
		repo.GetStorageBackend() != string(gitrepo.StorageBackendAsc) ||
		!repo.CanEnablePulls() || !ctx.Repo.CanRead(unit.TypePullRequests) ||
		// the editor commits to the stack's head, which needs write access to the code
		!ctx.Repo.CanWrite(unit.TypeCode) {
		return
	}
	prs, err := issues_model.GetUnmergedPullRequestsByBaseInfo(ctx, repo.ID, ctx.Repo.BranchName)
	if err != nil {
		log.Warn("the open stacks of %s are left out of the Propose change bar: %v", repo.FullName(), err)
		return
	}
	mine := make([]*issues_model.PullRequest, 0, len(prs))
	for _, pr := range prs {
		if pr.HeadRepoID != repo.ID {
			continue
		}
		if err := pr.LoadIssue(ctx); err != nil {
			log.Warn("the open stacks of %s: issue of pull %d: %v", repo.FullName(), pr.ID, err)
			continue
		}
		mine = append(mine, pr)
	}
	stacks := ascMyStacks(mine, repo.ID, ctx.Doer.ID, func(branch string) string {
		return ctx.Repo.RepoLink + "/_edit/" + util.PathEscapeSegments(branch) + "/" + util.PathEscapeSegments(ctx.Repo.TreePath)
	})
	if len(stacks) > 0 {
		ctx.Data["AscMyStacks"] = stacks
	}
}
