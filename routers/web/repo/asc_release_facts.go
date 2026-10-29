// Copyright 2026 The ASC Hub Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package repo

import (
	"strings"

	"forgejo.org/models/db"
	git_model "forgejo.org/models/git"
	repo_model "forgejo.org/models/repo"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/git"
	"forgejo.org/modules/log"
	"forgejo.org/services/context"

	"xorm.io/builder"
)

// What a release shows besides its note (board Releases): the changes since the release before it grouped as Added,
// Changed and Fixed from the first words of each intent, how many were written by agents, and the check results of
// the commit it was cut from. Everything is read from the repository that holds the release; the caller shows it
// only to a viewer who may read the code and the releases.

// relMaxChanges bounds the changes listed for one release; the count is the whole number.
const relMaxChanges = 100

// Release categories, in the order the notes show them.
const (
	relAdded   = "added"
	relChanged = "changed"
	relFixed   = "fixed"
)

// relChange is one change in a release: who wrote it, its short id and its intent.
type relChange struct {
	Sha      string
	Summary  string
	Author   string
	User     *user_model.User
	Agent    bool
	Category string
}

// relGroup is the changes of one category.
type relGroup struct {
	Category string
	Changes  []*relChange
}

// relChanges is what a release card shows about its changes.
type relChanges struct {
	Previous string // the tag the changes are counted from
	Total    int
	Agents   int // changes written by agents among the listed ones
	Groups   []*relGroup
}

// relCategoryOf sorts an intent into Added, Changed or Fixed by its first word or its conventional prefix.
func relCategoryOf(summary string) string {
	s := strings.ToLower(strings.TrimSpace(summary))
	if i := strings.IndexAny(s, ":("); i > 0 && i <= 12 {
		s = s[:i]
	}
	word := s
	if i := strings.IndexAny(word, " \t"); i > 0 {
		word = word[:i]
	}
	switch word {
	case "fix", "fixes", "fixed", "fixing", "bugfix", "hotfix", "repair", "repairs", "patch":
		return relFixed
	case "add", "adds", "added", "adding", "feat", "feature", "new", "introduce", "introduces", "implement", "implements", "support", "create", "creates":
		return relAdded
	}
	return relChanged
}

// parseRelLog reads `git log --format=%H%x00%aN%x00%aE%x00%s` into changes. A line that does not parse is skipped.
func parseRelLog(out string) []*relChange {
	var changes []*relChange
	for _, line := range strings.Split(out, "\n") {
		p := strings.Split(line, "\x00")
		if len(p) != 4 || p[0] == "" {
			continue
		}
		changes = append(changes, &relChange{Sha: p[0], Author: p[1], Summary: p[3], Category: relCategoryOf(p[3])})
	}
	return changes
}

// groupRelChanges puts changes in the groups Added, Changed, Fixed; an empty group is left out.
func groupRelChanges(changes []*relChange) []*relGroup {
	var groups []*relGroup
	for _, cat := range []string{relAdded, relChanged, relFixed} {
		g := &relGroup{Category: cat}
		for _, c := range changes {
			if c.Category == cat {
				g.Changes = append(g.Changes, c)
			}
		}
		if len(g.Changes) > 0 {
			groups = append(groups, g)
		}
	}
	return groups
}

// previousReleaseTag is the tag of the release before r (a published release with a tag, older than r), or "".
func previousReleaseTag(ctx *context.Context, r *repo_model.Release) string {
	prev := new(repo_model.Release)
	has, err := db.GetEngine(ctx).Where(builder.Eq{"repo_id": r.RepoID, "is_draft": false, "is_tag": false}).
		And(builder.Lt{"created_unix": r.CreatedUnix}).And(builder.Neq{"sha1": ""}).
		OrderBy("created_unix DESC, id DESC").Get(prev)
	if err != nil || !has {
		return ""
	}
	return prev.TagName
}

// releaseChangesOf reads the changes between the release before r and r. It returns nil for a draft, a bare tag, a
// release without a commit, or when git fails (the card then shows no list instead of a wrong one).
func releaseChangesOf(ctx *context.Context, r *repo_model.Release) *relChanges {
	if r.IsDraft || r.IsTag || r.Sha1 == "" || ctx.Repo.GitRepo == nil {
		return nil
	}
	prev := previousReleaseTag(ctx, r)
	rng := r.Sha1
	if prev != "" {
		rng = "refs/tags/" + prev + ".." + r.Sha1
	}
	cnt, _, err := git.NewCommand(ctx, "rev-list", "--count", "--no-merges").AddDynamicArguments(rng).RunStdString(&git.RunOpts{Dir: ctx.Repo.GitRepo.Path})
	if err != nil {
		log.Error("release changes of %s %s: %v", ctx.Repo.Repository.FullName(), r.TagName, err)
		return nil
	}
	out, _, err := git.NewCommand(ctx, "log", "--no-merges", "--format=%H%x00%aN%x00%aE%x00%s", "-n", "100").AddDynamicArguments(rng).RunStdString(&git.RunOpts{Dir: ctx.Repo.GitRepo.Path})
	if err != nil {
		return nil
	}
	total := 0
	for _, c := range strings.TrimSpace(cnt) {
		if c < '0' || c > '9' {
			return nil
		}
		total = total*10 + int(c-'0')
	}
	changes := parseRelLog(out)
	emails := map[string]string{}
	for _, line := range strings.Split(out, "\n") {
		if p := strings.Split(line, "\x00"); len(p) == 4 {
			emails[p[0]] = p[2]
		}
	}
	users := map[string]*user_model.User{}
	agents := 0
	for _, c := range changes {
		email := strings.ToLower(emails[c.Sha])
		u, seen := users[email]
		if !seen {
			if got, err := user_model.GetUserByEmail(ctx, email); err == nil {
				u = got
			}
			users[email] = u
		}
		c.User = u
		if u != nil && u.IsBot() {
			c.Agent = true
			agents++
		}
	}
	return &relChanges{Previous: prev, Total: total, Agents: agents, Groups: groupRelChanges(changes)}
}

// relChecks counts the latest result of each check of a commit: how many passed of how many.
func relChecks(statuses []*git_model.CommitStatus) (pass, total int) {
	p, f, w := checksCounts(statuses)
	return p, p + f + w
}
