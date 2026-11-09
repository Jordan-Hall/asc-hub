// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repoquick

import (
	git_model "forgejo.org/models/git"
	access_model "forgejo.org/models/perm/access"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unit"
	"forgejo.org/modules/log"
	"forgejo.org/services/context"
)

// The check states of a row.
const (
	CheckPassing = "passing"
	CheckFailing = "failing"
	CheckRunning = "running"
	CheckNone    = "none"
)

// RowCheck is the check status of the default branch of one row: State is one of the Check states, Failing is how
// many checks failed (or errored) when State is CheckFailing.
type RowCheck struct {
	State   string
	Failing int
}

// rowCheckOf sums up the latest status of each check (context) of a commit.
func rowCheckOf(statuses []*git_model.CommitStatus) RowCheck {
	if len(statuses) == 0 {
		return RowCheck{State: CheckNone}
	}
	c := RowCheck{State: CheckPassing}
	running := false
	for _, s := range statuses {
		switch {
		case s.State.IsFailure() || s.State.IsError():
			c.Failing++
		case s.State.IsPending():
			running = true
		}
	}
	switch {
	case c.Failing > 0:
		c.State = CheckFailing
	case running:
		c.State = CheckRunning
	}
	return c
}

// LoadChecks gives the rows of the list the check status of their default branch (ctx.Data["AscRowChecks"], keyed by
// repository id). A row whose code the viewer may not read, or that is empty, has no entry and shows nothing; checks
// name commits and tests, which Forgejo shows only to a reader of the code. A failure is logged and no row shows it.
func LoadChecks(ctx *context.Context, repos repo_model.RepositoryList) {
	branches := make(map[int64]string, len(repos))
	for _, r := range repos {
		if r.IsEmpty || r.DefaultBranch == "" {
			continue
		}
		p, err := access_model.GetUserRepoPermission(ctx, r, ctx.Doer)
		if err != nil {
			log.Error("repository list: the permission of %s (request %s): %v", r.FullName(), ctx.RequestIDForLog(), err)
			return
		}
		if p.CanRead(unit.TypeCode) {
			branches[r.ID] = r.DefaultBranch
		}
	}
	if len(branches) == 0 {
		return
	}
	heads, err := git_model.FindBranchesByRepoAndBranchName(ctx, branches)
	if err != nil {
		log.Error("repository list: the default branches (request %s): %v", ctx.RequestIDForLog(), err)
		return
	}
	pairs := make([]git_model.RepoSHA, 0, len(heads))
	for id, sha := range heads {
		pairs = append(pairs, git_model.RepoSHA{RepoID: id, SHA: sha})
	}
	statuses, err := git_model.GetLatestCommitStatusForPairs(ctx, pairs)
	if err != nil {
		log.Error("repository list: the commit statuses (request %s): %v", ctx.RequestIDForLog(), err)
		return
	}
	checks := make(map[int64]RowCheck, len(heads))
	for id := range heads {
		checks[id] = rowCheckOf(statuses[id])
	}
	ctx.Data["AscRowChecks"] = checks
}

// LoadWatching gives a signed-in viewer the rows they watch (ctx.Data["WatchedRepos"], keyed by repository id); the
// template shows the Watch toggle only when the map was made.
func LoadWatching(ctx *context.Context, repos repo_model.RepositoryList) {
	if ctx.Doer == nil {
		return
	}
	watched := make(map[int64]bool, len(repos))
	for _, r := range repos {
		watched[r.ID] = repo_model.IsWatcher(ctx, ctx.Doer.ID, r.ID)
	}
	ctx.Data["WatchedRepos"] = watched
}
