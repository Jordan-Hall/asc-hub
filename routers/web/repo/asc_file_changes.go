// Copyright 2026 The ASC Hub Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package repo

import (
	"fmt"

	issues_model "forgejo.org/models/issues"
	"forgejo.org/models/unit"
	"forgejo.org/modules/gitrepo"
	"forgejo.org/modules/log"
	"forgejo.org/modules/setting"
	"forgejo.org/services/context"
)

// "Open changes touching this file" of the file view (board Code): the open stacked changes whose diff names the
// file being viewed. Trust: only a viewer who may read the pull requests of the repository gets the card (the
// titles and links belong to that unit), and only the open pull requests of this repository are looked at.

const (
	// fileChangesScan bounds the open pull requests whose changed files are read for one file view.
	fileChangesScan = 30
	// fileChangesShown bounds the rows of the card.
	fileChangesShown = 3
)

// fileChangeRow is one open change that touches the file.
type fileChangeRow struct {
	Stack string
	Title string
	Link  string
	Draft bool
}

// prepareAscFileChanges sets AscFileChanges for the file in view.
func prepareAscFileChanges(ctx *context.Context) {
	repo := ctx.Repo.Repository
	if !setting.Asc.Enabled || repo == nil || repo.GetStorageBackend() != string(gitrepo.StorageBackendAsc) ||
		!repo.CanEnablePulls() || !ctx.Repo.CanRead(unit.TypeCode) || !ctx.Repo.CanRead(unit.TypePullRequests) ||
		ctx.Repo.TreePath == "" || ctx.Repo.GitRepo == nil {
		return
	}
	prs, err := issues_model.ListOpenAscPullRequests(ctx, repo.ID, fileChangesScan)
	if err != nil {
		log.Error("file changes: open pull requests of %s: %v", repo.FullName(), err)
		return
	}
	var rows []*fileChangeRow
	for _, pr := range prs {
		if len(rows) >= fileChangesShown {
			break
		}
		if pr.MergeBase == "" {
			continue
		}
		head, err := ctx.Repo.GitRepo.GetRefCommitID(pr.GetGitRefName())
		if err != nil {
			continue
		}
		files, err := ctx.Repo.GitRepo.GetFilesChangedBetween(pr.MergeBase, head)
		if err != nil {
			continue
		}
		touches := false
		for _, f := range files {
			if f == ctx.Repo.TreePath {
				touches = true
				break
			}
		}
		if !touches {
			continue
		}
		if err := pr.LoadIssue(ctx); err != nil {
			continue
		}
		rows = append(rows, &fileChangeRow{
			Stack: ascStackLabel(pr.HeadBranch, pr.Issue.Title),
			Title: pr.Issue.Title,
			Link:  fmt.Sprintf("%s/pulls/%d", ctx.Repo.RepoLink, pr.Issue.Index),
			Draft: pr.IsWorkInProgress(ctx),
		})
	}
	if len(rows) > 0 {
		ctx.Data["AscFileChanges"] = rows
	}
}
