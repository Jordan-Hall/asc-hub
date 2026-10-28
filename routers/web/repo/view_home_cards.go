// Copyright 2026 The ASC Hub Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package repo

import (
	gocontext "context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"forgejo.org/models/db"
	git_model "forgejo.org/models/git"
	issues_model "forgejo.org/models/issues"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unit"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/git"
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

// homeActivityWeeks is how many weeks the Activity card covers.
const homeActivityWeeks = 12

// HomeContributor is one row of the Contributors card: who landed the most changes in the weeks of the Activity card.
type HomeContributor struct {
	Name  string
	Email string
	Count int
	Pct   int // of the most active contributor, for the bar
	User  *user_model.User
}

// homeContributorLimit is how many people the Contributors card lists.
const homeContributorLimit = 5

// parseActivityLog reads the output of `git log --format=%ct%x00%aN%x00%aE`: the times of the changes and the people
// who made them, most changes first (ties by name), at most limit of them. A line that does not parse is skipped.
func parseActivityLog(out string, limit int) (times []int64, people []HomeContributor) {
	byEmail := map[string]*HomeContributor{}
	var order []string
	for _, line := range strings.Split(out, "\n") {
		parts := strings.Split(line, "\x00")
		if len(parts) != 3 {
			continue
		}
		t, err := strconv.ParseInt(strings.TrimSpace(parts[0]), 10, 64)
		if err != nil {
			continue
		}
		times = append(times, t)
		key := strings.ToLower(parts[2])
		if c, ok := byEmail[key]; ok {
			c.Count++
			continue
		}
		byEmail[key] = &HomeContributor{Name: parts[1], Email: parts[2], Count: 1}
		order = append(order, key)
	}
	for _, k := range order {
		people = append(people, *byEmail[k])
	}
	sort.SliceStable(people, func(i, j int) bool {
		if people[i].Count != people[j].Count {
			return people[i].Count > people[j].Count
		}
		return people[i].Name < people[j].Name
	})
	if len(people) > limit {
		people = people[:limit]
	}
	if len(people) > 0 {
		most := people[0].Count
		for i := range people {
			people[i].Pct = people[i].Count * 100 / most
		}
	}
	return times, people
}

// HomeActivity is the Activity card: the changes landed per week on the viewed ref, oldest week first.
type HomeActivity struct {
	Total  int
	Counts []int
	Points string // the points of the sparkline in a 0..100 by 0..24 box
}

// weeklyCounts buckets unix times into weeks of seven days that end at now; the last bucket is the current week. A time
// outside the window is ignored.
func weeklyCounts(times []int64, now time.Time, weeks int) []int {
	counts := make([]int, weeks)
	const week = 7 * 24 * 3600
	for _, t := range times {
		age := now.Unix() - t
		if age < 0 || age >= int64(weeks)*week {
			continue
		}
		counts[weeks-1-int(age/week)]++
	}
	return counts
}

// sparkPoints turns counts into the points attribute of a polyline in a 100 by 24 box; a flat series is a flat line.
func sparkPoints(counts []int) string {
	if len(counts) < 2 {
		return ""
	}
	most := 0
	for _, c := range counts {
		most = max(most, c)
	}
	var sb strings.Builder
	for i, c := range counts {
		x := float64(i) * 100 / float64(len(counts)-1)
		y := 23.0
		if most > 0 {
			y = 23 - float64(c)*22/float64(most)
		}
		if i > 0 {
			sb.WriteByte(' ')
		}
		fmt.Fprintf(&sb, "%.1f,%.1f", x, y)
	}
	return sb.String()
}

// prepareHomeActivity counts the changes landed per week on the viewed ref with one git log. It needs only code
// access, like the list of changes beside it; a failure leaves the card out.
func prepareHomeActivity(ctx *context.Context) {
	if ctx.Repo.GitRepo == nil || ctx.Repo.Commit == nil {
		return
	}
	since := time.Now().Add(-time.Duration(homeActivityWeeks) * 7 * 24 * time.Hour)
	out, _, err := git.NewCommand(ctx, "log", "--format=%ct%x00%aN%x00%aE").AddOptionFormat("--since=%d", since.Unix()).AddDynamicArguments(ctx.Repo.Commit.ID.String()).
		RunStdString(&git.RunOpts{Dir: ctx.Repo.GitRepo.Path})
	if err != nil {
		return
	}
	times, people := parseActivityLog(out, homeContributorLimit)
	for i := range people {
		if u, err := user_model.GetUserByEmail(ctx, people[i].Email); err == nil {
			people[i].User = u
		}
	}
	ctx.Data["HomeContributors"] = people
	counts := weeklyCounts(times, time.Now(), homeActivityWeeks)
	total := 0
	for _, c := range counts {
		total += c
	}
	ctx.Data["HomeActivity"] = &HomeActivity{Total: total, Counts: counts, Points: sparkPoints(counts)}
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

	prepareHomeActivity(ctx)
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
