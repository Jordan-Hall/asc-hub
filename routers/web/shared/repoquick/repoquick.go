// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

// Package repoquick reads the quick chips of the repository lists (Has open stacks, Agent activity, Starred), which
// templates/shared/repo_search.tmpl shows next to Templates and Archived on every list it is the filter bar of.
package repoquick

import (
	"time"

	repo_model "forgejo.org/models/repo"
	"forgejo.org/modules/setting"
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
