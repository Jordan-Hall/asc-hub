// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

// Package repoquick reads the quick chips of the repository lists (Has open stacks, Agent activity, Starred), which
// templates/shared/repo_search.tmpl shows next to Templates and Archived on every list it is the filter bar of.
package repoquick

import (
	"time"

	activities_model "forgejo.org/models/activities"
	repo_model "forgejo.org/models/repo"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/log"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/timeutil"
	"forgejo.org/services/context"
)

// Now is the clock of the Agent activity window; tests replace it.
var Now = time.Now

// Read reads the quick chips of the request (open_stacks, agents, starred) and gives the page what the filter bar
// needs: which chips are on, whether Starred is offered (a signed-in viewer on a hub with stars) and the label of the
// first chip that is on, for the no-results state.
func Read(ctx *context.Context) repo_model.AscQuickFilter {
	starredOffered := ctx.Doer != nil && !setting.Repository.DisableStars
	q := repo_model.AscQuickFilter{OpenStacks: ctx.FormBool("open_stacks")}
	agents := ctx.FormBool("agents")
	if agents {
		q.AgentsSince = repo_model.AgentActivitySince(Now())
	}
	starred := starredOffered && ctx.FormBool("starred")
	if starred {
		q.StarredBy = ctx.Doer.ID
	}
	ctx.Data["AscQuickOpenStacks"] = q.OpenStacks
	ctx.Data["AscQuickAgents"] = agents
	ctx.Data["AscQuickStarred"] = starred
	ctx.Data["AscQuickStarredOffered"] = starredOffered
	ctx.Data["AscQuickAgentDays"] = repo_model.AscAgentActivityDays
	switch {
	case q.OpenStacks:
		ctx.Data["AscQuickLabel"] = ctx.Locale.TrString("explore.quick_open_stacks")
	case agents:
		ctx.Data["AscQuickLabel"] = ctx.Locale.TrString("explore.quick_agents")
	case starred:
		ctx.Data["AscQuickLabel"] = ctx.Locale.TrString("explore.quick_starred")
	}
	return q
}

// KeepOnPages keeps the chips that are on in the links of the pager of the list.
func KeepOnPages(pager *context.Pagination, q repo_model.AscQuickFilter) {
	if q.OpenStacks {
		pager.AddParamString("open_stacks", "1")
	}
	if q.AgentsSince > 0 {
		pager.AddParamString("agents", "1")
	}
	if q.StarredBy > 0 {
		pager.AddParamString("starred", "1")
	}
}

// AscContributorMax is how many avatars the contributor stack of a row shows.
const AscContributorMax = 4

// Contributors is the contributor stack of one row: the most active people and agents of the window the viewer can
// see, and how many of them acted there in all.
type Contributors struct {
	Users []*user_model.User
	Total int
}

// LoadContributors gives the rows of the list their contributor stack (ctx.Data["AscContributors"], keyed by
// repository id): who acted in the repository in the last activities.AscContributorDays days, most active first,
// agents (bot accounts) with people. A user who keeps their activity private, or whose profile the viewer may not
// see, is left out (an admin and the user see them). A failure is logged and the rows show no stack.
func LoadContributors(ctx *context.Context, repos repo_model.RepositoryList) {
	if len(repos) == 0 {
		return
	}
	ids := make([]int64, 0, len(repos))
	for _, r := range repos {
		ids = append(ids, r.ID)
	}
	since := timeutil.TimeStamp(Now().AddDate(0, 0, -activities_model.AscContributorDays).Unix())
	top, err := activities_model.AscTopContributors(ctx, ids, since)
	if err != nil {
		log.Error("repository list: loading the contributors (request %s): %v", ctx.RequestIDForLog(), err)
		return
	}
	stacks := make(map[int64]*Contributors, len(top))
	for repoID, users := range top {
		c := &Contributors{}
		for _, u := range users {
			if !activities_model.ActivityReadable(u, ctx.Doer) || !user_model.IsUserVisibleToViewer(ctx, u, ctx.Doer) {
				continue
			}
			c.Total++
			if len(c.Users) < AscContributorMax {
				c.Users = append(c.Users, u)
			}
		}
		if c.Total > 0 {
			stacks[repoID] = c
		}
	}
	ctx.Data["AscContributors"] = stacks
	ctx.Data["AscContributorDays"] = activities_model.AscContributorDays
}
