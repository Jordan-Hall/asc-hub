// Copyright 2026 The ASC Hub Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package repo

import (
	"forgejo.org/models/db"
	git_model "forgejo.org/models/git"
	issues_model "forgejo.org/models/issues"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unit"
	"forgejo.org/modules/log"
	"forgejo.org/modules/optional"
	"forgejo.org/services/context"
)

// homeCardLimit is how many rows the repository home shows in each of its two lists.
const homeCardLimit = 5

// prepareHomeCards loads what the repository home shows besides the files: the open stacks (pull requests), the
// latest changes of the viewed ref and the latest release. A failure is logged and the card stays out.
func prepareHomeCards(ctx *context.Context) {
	if ctx.Repo.TreePath != "" || ctx.Repo.Commit == nil {
		return
	}
	repo := ctx.Repo.Repository

	if repo.CanEnablePulls() && ctx.Repo.CanRead(unit.TypePullRequests) {
		pulls, err := issues_model.Issues(ctx, &issues_model.IssuesOptions{
			Paginator: &db.ListOptions{Page: 1, PageSize: homeCardLimit},
			RepoIDs:   []int64{repo.ID},
			IsPull:    optional.Some(true),
			IsClosed:  optional.Some(false),
			SortType:  "recentupdate",
		})
		if err != nil {
			log.Error("home: open pulls of %s: %v", repo.FullName(), err)
		} else {
			ctx.Data["HomePulls"] = pulls
		}
	}

	commits, err := ctx.Repo.Commit.CommitsByRange(1, homeCardLimit, "")
	if err != nil {
		log.Error("home: latest commits of %s: %v", repo.FullName(), err)
	} else {
		ctx.Data["HomeCommits"] = git_model.ParseCommitsWithStatus(ctx, commits, repo)
	}

	if ctx.Repo.CanRead(unit.TypeReleases) {
		rel, err := repo_model.GetLatestReleaseByRepoID(ctx, repo.ID)
		if err == nil {
			ctx.Data["HomeRelease"] = rel
		} else if !repo_model.IsErrReleaseNotExist(err) {
			log.Error("home: latest release of %s: %v", repo.FullName(), err)
		}
	}
}
