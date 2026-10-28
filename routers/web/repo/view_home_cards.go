// Copyright 2026 The ASC Hub Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package repo

import (
	gocontext "context"
	"fmt"
	"strings"

	"forgejo.org/models/db"
	git_model "forgejo.org/models/git"
	issues_model "forgejo.org/models/issues"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unit"
	"forgejo.org/modules/log"
	"forgejo.org/modules/optional"
	"forgejo.org/modules/setting"
	"forgejo.org/services/asctrust"
	"forgejo.org/services/context"
)

// homeCardLimit is how many rows the repository home shows in each of its two lists.
const homeCardLimit = 5

// homeLanded is one row of "Recently landed": the commit with its checks, the size of the change and the stack
// (pull request) it landed through when the hub knows one. HasStat is false when the size could not be read.
type homeLanded struct {
	*git_model.SignCommitWithStatuses
	Additions, Deletions int
	HasStat              bool
	StackName            string
	StackLink            string
}

// licenseFileName returns the name of the licence file among the names of a root tree, or "". The match is the
// usual spelling of the file (LICENSE, LICENCE, COPYING, UNLICENSE, with or without an extension); it does not
// read the file, so the card names the file and does not claim a licence.
func licenseFileName(names []string) string {
	for _, n := range names {
		base := strings.ToUpper(n)
		if i := strings.IndexByte(base, '.'); i > 0 {
			base = base[:i]
		}
		switch base {
		case "LICENSE", "LICENCE", "COPYING", "UNLICENSE":
			return n
		}
	}
	return ""
}

// homeStackOf is the stack (pull request) a commit landed through: its head branch and link. It answers only when
// the viewer may read pull requests of the repository; a person with code access alone gets nothing, because the
// name of a head branch and the link belong to the pull request unit.
func homeStackOf(ctx gocontext.Context, repoID int64, repoLink, sha string, canPulls bool) (name, link string) {
	if !canPulls {
		return "", ""
	}
	pr, err := issues_model.GetPullRequestByMergedCommit(ctx, repoID, sha)
	if err != nil || pr.HeadBranch == "" {
		return "", ""
	}
	return pr.HeadBranch, fmt.Sprintf("%s/pulls/%d", repoLink, pr.Index)
}

// buildHomeLanded decorates commits with their size and stack. Each lookup that fails leaves its field empty.
func buildHomeLanded(ctx *context.Context, repo *repo_model.Repository, commits []*git_model.SignCommitWithStatuses, canPulls bool) []*homeLanded {
	rows := make([]*homeLanded, 0, len(commits))
	for _, c := range commits {
		row := &homeLanded{SignCommitWithStatuses: c}
		if ctx.Repo.GitRepo != nil {
			if _, add, del, err := ctx.Repo.GitRepo.GetCommitShortStat(c.ID.String()); err == nil {
				row.Additions, row.Deletions, row.HasStat = add, del, true
			}
		}
		row.StackName, row.StackLink = homeStackOf(ctx, repo.ID, repo.Link(), c.ID.String(), canPulls)
		rows = append(rows, row)
	}
	return rows
}

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
		withStatus := git_model.ParseCommitsWithStatus(ctx, commits, repo)
		ctx.Data["HomeCommits"] = withStatus
		ctx.Data["HomeLanded"] = buildHomeLanded(ctx, repo, withStatus, repo.CanEnablePulls() && ctx.Repo.CanRead(unit.TypePullRequests))
	}

	if entries, err := ctx.Repo.Commit.ListEntries(); err == nil {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			if !e.IsDir() {
				names = append(names, e.Name())
			}
		}
		ctx.Data["HomeLicenseFile"] = licenseFileName(names)
	}

	prepareHomePolicy(ctx, repo)

	if ctx.Repo.CanRead(unit.TypeReleases) {
		rel, err := repo_model.GetLatestReleaseByRepoID(ctx, repo.ID)
		if err == nil {
			ctx.Data["HomeRelease"] = rel
		} else if !repo_model.IsErrReleaseNotExist(err) {
			log.Error("home: latest release of %s: %v", repo.FullName(), err)
		}
	}
}

// prepareHomePolicy adds the Policy card: one read of the policy source with the credential of the viewer. The card
// stays out when asc is off, the repository is not stored in asc, the server lacks the policy service or the read fails.
func prepareHomePolicy(ctx *context.Context, repo *repo_model.Repository) {
	if !setting.Asc.Enabled || !ctx.IsSigned || ctx.Doer == nil || !asctrust.HasFeature(ctx, asctrust.FeaturePolicy) {
		return
	}
	name, err := asctrust.RepoName(repo)
	if err != nil {
		return
	}
	conn, err := asctrust.Conn()
	if err != nil {
		return
	}
	callCtx, release, err := asctrust.UserCall(ctx, ctx.Doer, asctrust.CallRepoRead, repo)
	if err != nil {
		return
	}
	defer release()
	sum, err := asctrust.LoadPolicySummary(callCtx, conn, name)
	if err != nil {
		log.Error("home: policy summary of %s: %v", repo.FullName(), err)
		return
	}
	ctx.Data["HomePolicy"] = sum
}
