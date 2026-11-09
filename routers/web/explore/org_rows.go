// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package explore

import (
	"forgejo.org/models/organization"
	repo_model "forgejo.org/models/repo"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/log"
	"forgejo.org/services/ascagentdir"
	"forgejo.org/services/context"
)

// exploreOrgStack is how many member avatars a row of the organization directory shows.
const exploreOrgStack = 4

// ExploreOrgMembers is the member stack of an organization row: the first members the viewer may see and how many
// more there are.
type ExploreOrgMembers struct {
	Users []*user_model.User
	More  int64
	Total int64
}

// loadExploreOrgRows puts what the rows of the organization directory show beside each organization into the page
// data: the member stack (ExploreOrgMembers, the public members, or all of them for a member or an administrator) and,
// for a viewer who gets agents, the number of agents that ran in the asc repositories of the organization the viewer
// can read (ExploreOrgAgents). A lookup that fails leaves that part out; the list itself is still shown.
func loadExploreOrgRows(ctx *context.Context, users []*user_model.User) {
	members := make(map[int64]*ExploreOrgMembers, len(users))
	for _, u := range users {
		if !u.IsOrganization() {
			continue
		}
		m, err := orgMemberStack(ctx, u.ID)
		if err != nil {
			log.Error("explore: the members of %s: %v", u.Name, err)
			continue
		}
		if m != nil {
			members[u.ID] = m
		}
	}
	ctx.Data["ExploreOrgMembers"] = members
	loadExploreOrgAgents(ctx, users)
}

func orgMemberStack(ctx *context.Context, orgID int64) (*ExploreOrgMembers, error) {
	opts := &organization.FindOrgMembersOpts{Doer: ctx.Doer, OrgID: orgID}
	if ctx.Doer != nil {
		isMember, err := organization.IsOrganizationMember(ctx, orgID, ctx.Doer.ID)
		if err != nil {
			return nil, err
		}
		opts.IsDoerMember = isMember
	}
	total, err := organization.CountOrgMembers(ctx, opts)
	if err != nil || total == 0 {
		return nil, err
	}
	opts.ListOptions.Page = 1
	opts.ListOptions.PageSize = exploreOrgStack
	ous, err := organization.GetOrgUsersByOrgID(ctx, opts)
	if err != nil {
		return nil, err
	}
	ids := make([]int64, 0, len(ous))
	for _, ou := range ous {
		ids = append(ids, ou.UID)
	}
	list, err := user_model.GetUsersByIDs(ctx, ids)
	if err != nil {
		return nil, err
	}
	return &ExploreOrgMembers{Users: list, More: max(total-int64(len(list)), 0), Total: total}, nil
}

// loadExploreOrgAgents counts the agents per organization of the page with one call to asc over at most
// ascagentdir.MaxRepos repositories, taken organization by organization in the order of the page.
func loadExploreOrgAgents(ctx *context.Context, users []*user_model.User) {
	if !ascagentdir.Enabled(ctx, ctx.Doer) {
		return
	}
	var repos []*repo_model.Repository
	for _, u := range users {
		if !u.IsOrganization() || len(repos) >= ascagentdir.MaxRepos {
			continue
		}
		found, err := ascagentdir.Repos(ctx, ctx.Doer, u.ID, false)
		if err != nil {
			log.Error("explore: the asc repositories of %s: %v", u.Name, err)
			return
		}
		repos = append(repos, found[:min(len(found), ascagentdir.MaxRepos-len(repos))]...)
	}
	agents, err := ascagentdir.Summarize(ctx, repos, "")
	if err != nil {
		log.Warn("explore: the agents of the organizations (request %s): %v", ctx.RequestIDForLog(), err)
		return
	}
	counts := map[int64]int{}
	for _, a := range agents {
		counts[a.Owner.ID]++
	}
	ctx.Data["ExploreOrgAgents"] = counts
}
