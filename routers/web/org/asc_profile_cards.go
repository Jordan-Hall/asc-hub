// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package org

import (
	"forgejo.org/models/organization"
	"forgejo.org/modules/log"
	"forgejo.org/modules/setting"
	"forgejo.org/services/ascagentdir"
	"forgejo.org/services/asctrust"
	"forgejo.org/services/context"
)

// orgCardAgents is how many agents the Agents card of an organization page names.
const orgCardAgents = 5

// orgMergePolicy returns the merge defaults of the organization for a member, read from asc with the rights of the
// viewer, or nil when asc is off, the viewer may not read them or asc could not be asked.
func orgMergePolicy(ctx *context.Context, org *organization.Organization) *asctrust.MergeDefaults {
	if !setting.Asc.Enabled || ctx.Doer == nil || !ctx.Org.IsMember || !asctrust.HasFeature(ctx, asctrust.FeatureOrgSettings) {
		return nil
	}
	conn, err := asctrust.Conn()
	if err != nil {
		return nil
	}
	callCtx, release, err := asctrust.UserCallOrg(ctx, ctx.Doer, org.AsUser())
	if err != nil {
		return nil
	}
	defer release()
	s, err := asctrust.LoadOrgSettings(callCtx, conn, org.Name)
	if err != nil {
		log.Debug("organization page: the merge defaults of %s: %v", org.Name, err)
		return nil
	}
	return &s.Merge
}

// orgRunAgents returns the agents that ran in the asc repositories of the organization the viewer can read, for a
// viewer who is not a member (a member sees the agents the organization keeps in asc). ok is false when the viewer
// gets no agents or asc could not be asked.
func orgRunAgents(ctx *context.Context, org *organization.Organization) ([]*ascagentdir.Agent, bool) {
	if ctx.Org.IsMember || !ascagentdir.Enabled(ctx, ctx.Doer) {
		return nil, false
	}
	repos, err := ascagentdir.Repos(ctx, ctx.Doer, org.ID, false)
	if err != nil {
		log.Error("organization page: the asc repositories of %s: %v", org.Name, err)
		return nil, false
	}
	agents, err := ascagentdir.Summarize(ctx, repos, "")
	if err != nil {
		log.Warn("organization page: the agents of %s (request %s): %v", org.Name, ctx.RequestIDForLog(), err)
		return nil, false
	}
	return ascagentdir.OfOwner(agents, org.ID), true
}

// loadOrgProfileCards fills the Agents and Merge policy cards of the organization page.
func loadOrgProfileCards(ctx *context.Context, org *organization.Organization) {
	if ctx.Org.IsMember {
		ctx.Data["OrgAgentList"] = orgAgents(ctx, org)
		ctx.Data["OrgAscOn"] = setting.Asc.Enabled
		ctx.Data["OrgMergePolicy"] = orgMergePolicy(ctx, org)
		return
	}
	if agents, ok := orgRunAgents(ctx, org); ok && len(agents) > 0 {
		ctx.Data["OrgRunAgents"] = agents[:min(len(agents), orgCardAgents)]
		ctx.Data["OrgRunAgentsTotal"] = len(agents)
	}
}
