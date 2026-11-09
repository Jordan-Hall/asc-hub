// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package user

import (
	"cmp"
	"slices"
	"strings"
	"time"

	activities_model "forgejo.org/models/activities"
	"forgejo.org/models/db"
	issues_model "forgejo.org/models/issues"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/git"
	"forgejo.org/modules/gitrepo"
	"forgejo.org/modules/log"
	"forgejo.org/modules/timeutil"
	"forgejo.org/services/context"

	"xorm.io/builder"
)

// "Knows best" on a profile (design board "Profile"): the directories the person changes and reviews most in the
// last six months, as a share of all the files they changed or reviewed. It comes from the hub alone: the pull
// requests the person opened, those of agent runs they started, and those of others they reviewed, each counted by
// the files it changes between its merge base and its head. Only pull requests the viewer may read are counted.

const (
	// ascKnowsMonths is the range Knows best looks back over.
	ascKnowsMonths = 6
	// ascKnowsPulls bounds the pull requests read for changes and, apart, for reviews.
	ascKnowsPulls = 30
	// ascKnowsFresh bounds the pull requests whose files are read from the repository in one page view; the
	// others are counted from what earlier views kept, or on a later view.
	ascKnowsFresh = 10
	// ascKnowsMax is how many directories the card shows.
	ascKnowsMax = 3
	// ascKnowsDepth is how many directory levels a path is cut to.
	ascKnowsDepth = 2
)

// AscKnows is a directory of Knows best.
type AscKnows struct {
	Path    string
	Share   int // percent of the files the person changed or reviewed
	Bar     int // Share rounded down to a step of 5, for the bar's class
	Changes int // files changed in it
	Reviews int // files reviewed in it
}

// ascKnowsDir cuts a file path to its first ascKnowsDepth directories, with a slash at the end; a file at the
// root is "/".
func ascKnowsDir(path string) string {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	parts = parts[:len(parts)-1]
	if len(parts) == 0 {
		return "/"
	}
	return strings.Join(parts[:min(len(parts), ascKnowsDepth)], "/") + "/"
}

// ascKnowsDirs counts the files per directory.
func ascKnowsDirs(files []string) map[string]int {
	dirs := map[string]int{}
	for _, f := range files {
		if f != "" {
			dirs[ascKnowsDir(f)]++
		}
	}
	return dirs
}

// ascKnowsRank turns the directories of the changes and of the reviews into the card's rows.
func ascKnowsRank(changes, reviews map[string]int) []AscKnows {
	total := 0
	rows := map[string]*AscKnows{}
	add := func(dirs map[string]int, review bool) {
		for d, n := range dirs {
			r := rows[d]
			if r == nil {
				r = &AscKnows{Path: d}
				rows[d] = r
			}
			if review {
				r.Reviews += n
			} else {
				r.Changes += n
			}
			total += n
		}
	}
	add(changes, false)
	add(reviews, true)
	if total == 0 {
		return nil
	}
	out := make([]AscKnows, 0, len(rows))
	for _, r := range rows {
		r.Share = (100*(r.Changes+r.Reviews) + total/2) / total
		r.Bar = r.Share / 5 * 5
		out = append(out, *r)
	}
	slices.SortFunc(out, func(a, b AscKnows) int {
		if c := cmp.Compare(b.Changes+b.Reviews, a.Changes+a.Reviews); c != 0 {
			return c
		}
		return cmp.Compare(a.Path, b.Path)
	})
	return out[:min(len(out), ascKnowsMax)]
}

// ascKnowsPullsOf lists the readable pull requests of the person's changes (review false) or reviews (review true)
// since the given time, newest first.
func ascKnowsPullsOf(ctx *context.Context, person, doer *user_model.User, since timeutil.TimeStamp, review bool) ([]*issues_model.PullRequest, error) {
	var cond builder.Cond
	if review {
		cond = builder.In("issue.id", builder.Select("review.issue_id").From("review").Where(
			builder.Eq{"review.reviewer_id": person.ID}.
				And(builder.In("review.type", issues_model.ReviewTypeApprove, issues_model.ReviewTypeComment, issues_model.ReviewTypeReject)).
				And(builder.Gte{"review.created_unix": since}))).
			And(builder.Neq{"issue.poster_id": person.ID})
	} else {
		cond = builder.Gte{"issue.created_unix": since}.And(builder.Or(
			builder.Eq{"issue.poster_id": person.ID},
			builder.In("pull_request.id", builder.Select("pull_id").From("pull_request_asc_run").Where(builder.Eq{"LOWER(started_by)": person.LowerName})),
		))
	}
	pulls := make([]*issues_model.PullRequest, 0, ascKnowsPulls)
	return pulls, db.GetEngine(ctx).
		Join("INNER", "issue", "issue.id = pull_request.issue_id").
		Where(cond.And(ascReadablePulls(doer))).
		OrderBy("issue.updated_unix DESC, pull_request.id DESC").
		Limit(ascKnowsPulls).
		Find(&pulls)
}

// ascKnowsFiles gives the directories each pull request changes: kept ones when their head is still the head,
// otherwise read from the repository, at most ascKnowsFresh of them. A pull request that cannot be read is left out.
func ascKnowsFiles(ctx *context.Context, pulls []*issues_model.PullRequest) map[int64]map[string]int {
	ids := make([]int64, len(pulls))
	for i, pr := range pulls {
		ids[i] = pr.ID
	}
	out := make(map[int64]map[string]int, len(pulls))
	kept, err := issues_model.GetPullRequestAscDirs(ctx, ids)
	if err != nil {
		log.Error("asc knows: kept dirs: %v", err)
		return out
	}
	repos := map[int64]*git.Repository{}
	defer func() {
		for _, r := range repos {
			r.Close()
		}
	}()
	fresh := 0
	for _, pr := range pulls {
		if err := pr.LoadBaseRepo(ctx); err != nil || pr.BaseRepo == nil {
			continue
		}
		repo := repos[pr.BaseRepoID]
		if repo == nil {
			if repo, err = gitrepo.OpenRepository(ctx, pr.BaseRepo); err != nil {
				log.Debug("asc knows: open %d: %v", pr.BaseRepoID, err)
				continue
			}
			repos[pr.BaseRepoID] = repo
		}
		head, err := repo.GetRefCommitID(pr.GetGitRefName())
		if err != nil {
			continue
		}
		if k := kept[pr.ID]; k != nil && k.Head == head {
			out[pr.ID] = k.Dirs
			continue
		}
		if pr.MergeBase == "" || fresh >= ascKnowsFresh {
			continue
		}
		fresh++
		files, err := repo.GetFilesChangedBetween(pr.MergeBase, head)
		if err != nil {
			log.Debug("asc knows: files of pull %d: %v", pr.ID, err)
			continue
		}
		dirs := ascKnowsDirs(files)
		if err := issues_model.SetPullRequestAscDirs(ctx, pr.ID, head, dirs); err != nil {
			log.Error("asc knows: keep dirs of pull %d: %v", pr.ID, err)
		}
		out[pr.ID] = dirs
	}
	return out
}

// ascProfileKnows is Knows best of the person for the viewer, or nil when there is nothing to show or the person
// keeps their activity from the viewer.
func ascProfileKnows(ctx *context.Context, person, doer *user_model.User) []AscKnows {
	if !activities_model.ActivityReadable(person, doer) {
		return nil
	}
	since := timeutil.TimeStamp(time.Now().AddDate(0, -ascKnowsMonths, 0).Unix())
	sum := func(review bool) map[string]int {
		pulls, err := ascKnowsPullsOf(ctx, person, doer, since, review)
		if err != nil {
			log.Error("asc knows: pulls: %v", err)
			return nil
		}
		dirs := map[string]int{}
		for _, d := range ascKnowsFiles(ctx, pulls) {
			for k, n := range d {
				dirs[k] += n
			}
		}
		return dirs
	}
	return ascKnowsRank(sum(false), sum(true))
}
