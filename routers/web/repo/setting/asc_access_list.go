// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package setting

import (
	"net/http"
	"slices"
	"strings"

	auth_model "forgejo.org/models/auth"
	"forgejo.org/models/db"
	"forgejo.org/models/organization"
	"forgejo.org/models/perm"
	repo_model "forgejo.org/models/repo"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/optional"
	"forgejo.org/modules/timeutil"
	"forgejo.org/services/context"
)

// The access page (board RepoSettingsAccess) lists the people, the teams and the agents of a repository in one
// list, each with its role. The list can be narrowed with ?show=people (people and teams) or ?show=agents.

// The roles of the access page, as the role menus send them.
const (
	ascRoleRead     = "read"
	ascRoleWrite    = "write"
	ascRoleMaintain = ascMaintainMode
	ascRoleAdmin    = "admin"
)

// ascAccessRoles are the roles a person or an agent can be given here, highest first.
var ascAccessRoles = []string{ascRoleAdmin, ascRoleMaintain, ascRoleWrite, ascRoleRead}

// The kinds of the rows of the access list.
const (
	ascAccessPerson = "person"
	ascAccessAgent  = "agent"
	ascAccessTeam   = "team"
)

// The filters of the access list.
const (
	ascShowAll    = "all"
	ascShowPeople = "people"
	ascShowAgents = "agents"
)

// AscAccessRow is a row of the access list: a collaborator (a person or an agent) or a team.
type AscAccessRow struct {
	Kind         string
	Collaborator *repo_model.Collaborator // a person or an agent
	Team         *organization.Team       // a team
	Role         string                   // ascAccessRoles, or "owner" for a team of owners
	LastActive   timeutil.TimeStamp       // a person or an agent: the last sign-in or token use; 0 when never
}

// ascCollaboratorRole is the role of a collaboration as the access page names it.
func ascCollaboratorRole(c *repo_model.Collaboration) string {
	switch {
	case c.Mode == perm.AccessModeWrite && c.AscMaintain:
		return ascRoleMaintain
	case c.Mode >= perm.AccessModeAdmin:
		return ascRoleAdmin
	case c.Mode == perm.AccessModeWrite:
		return ascRoleWrite
	case c.Mode == perm.AccessModeRead:
		return ascRoleRead
	default:
		return ""
	}
}

// ascTeamRole is the role of a team as the access page names it; a team keeps the access mode of the team.
func ascTeamRole(t *organization.Team) string {
	switch {
	case t.AccessMode >= perm.AccessModeOwner:
		return "owner"
	case t.AccessMode >= perm.AccessModeAdmin:
		return ascRoleAdmin
	case t.AccessMode == perm.AccessModeWrite:
		return ascRoleWrite
	case t.AccessMode == perm.AccessModeRead:
		return ascRoleRead
	default:
		return ""
	}
}

// ascLastActive is, per user, the latest of the last sign-in and the last use of an access token.
func ascLastActive(ctx *context.Context, users []*repo_model.Collaborator) (map[int64]timeutil.TimeStamp, error) {
	last := make(map[int64]timeutil.TimeStamp, len(users))
	ids := make([]int64, 0, len(users))
	for _, u := range users {
		last[u.ID] = u.LastLoginUnix
		ids = append(ids, u.ID)
	}
	if len(ids) == 0 {
		return last, nil
	}
	var used []struct {
		UID  int64              `xorm:"uid"`
		Last timeutil.TimeStamp `xorm:"last"`
	}
	if err := db.GetEngine(ctx).Table(new(auth_model.AccessToken)).Select("uid, MAX(updated_unix) AS last").
		In("uid", ids).GroupBy("uid").Find(&used); err != nil {
		return nil, err
	}
	for _, u := range used {
		if u.Last > last[u.UID] {
			last[u.UID] = u.Last
		}
	}
	return last, nil
}

// ascAccessShow is the filter of the request, ascShowAll when it names none.
func ascAccessShow(ctx *context.Context) string {
	switch show := ctx.FormString("show"); show {
	case ascShowPeople, ascShowAgents:
		return show
	default:
		return ascShowAll
	}
}

// prepareAscAccessRows puts the access list in place: Data["AccessRows"] holds the rows of the filter of the
// request, Data["AccessShow"] the filter and Data["AccessCount"] the number of rows of each filter.
func prepareAscAccessRows(ctx *context.Context, users []*repo_model.Collaborator, teams []*organization.Team) bool {
	last, err := ascLastActive(ctx, users)
	if err != nil {
		ctx.ServerError("ascLastActive", err)
		return false
	}
	show := ascAccessShow(ctx)
	count := map[string]int{ascShowAll: len(users) + len(teams)}
	rows := make([]*AscAccessRow, 0, len(users)+len(teams))
	for _, u := range users {
		kind := ascAccessPerson
		if u.IsBot() {
			kind = ascAccessAgent
			count[ascShowAgents]++
		} else {
			count[ascShowPeople]++
		}
		if show == ascShowAgents && kind != ascAccessAgent || show == ascShowPeople && kind != ascAccessPerson {
			continue
		}
		rows = append(rows, &AscAccessRow{Kind: kind, Collaborator: u, Role: ascCollaboratorRole(u.Collaboration), LastActive: last[u.ID]})
	}
	count[ascShowPeople] += len(teams)
	if show != ascShowAgents {
		for _, t := range teams {
			rows = append(rows, &AscAccessRow{Kind: ascAccessTeam, Team: t, Role: ascTeamRole(t)})
		}
	}
	ctx.Data["AccessRows"] = rows
	ctx.Data["AccessShow"] = show
	ctx.Data["AccessCount"] = count
	ctx.Data["AccessRoles"] = ascAccessRoles
	ctx.Data["AccessShows"] = []string{ascShowAll, ascShowPeople, ascShowAgents}
	return true
}

// ascAccessResult is an entry of the combined search of the access page.
type ascAccessResult struct {
	Kind        string `json:"kind"`
	Name        string `json:"name"`
	FullName    string `json:"full_name"`
	AvatarURL   string `json:"avatar_url"`
	Description string `json:"description"`
}

// ascAccessSearchLimit is the number of people, of agents and of teams the combined search answers at most.
const ascAccessSearchLimit = 10

// AscAccessSearch answers the combined search of the access page (board RepoSettingsAccess: "Search people,
// teams or agents"): the people and agents of the instance and the teams of the owning organization whose name
// holds q.
func AscAccessSearch(ctx *context.Context) {
	q := ctx.FormTrim("q")
	results := []ascAccessResult{}
	if q == "" {
		ctx.JSON(http.StatusOK, map[string]any{"data": results})
		return
	}
	for _, typ := range []user_model.UserType{user_model.UserTypeIndividual, user_model.UserTypeBot} {
		users, _, err := user_model.SearchUsers(ctx, &user_model.SearchUserOptions{
			Actor:       ctx.Doer,
			Keyword:     q,
			Type:        typ,
			IsActive:    optional.Some(true),
			ListOptions: db.ListOptions{PageSize: ascAccessSearchLimit},
		})
		if err != nil {
			ctx.ServerError("SearchUsers", err)
			return
		}
		kind := ascAccessPerson
		if typ == user_model.UserTypeBot {
			kind = ascAccessAgent
		}
		for _, u := range users {
			if u.ID == ctx.Repo.Repository.OwnerID {
				continue
			}
			results = append(results, ascAccessResult{Kind: kind, Name: u.Name, FullName: u.FullName, AvatarURL: u.AvatarLink(ctx)})
		}
	}
	if ascSearchesTeams(ctx) {
		teams, _, err := organization.SearchTeam(ctx, &organization.SearchTeamOptions{
			Keyword:     q,
			OrgID:       ctx.Repo.Owner.ID,
			ListOptions: db.ListOptions{PageSize: ascAccessSearchLimit},
		})
		if err != nil {
			ctx.ServerError("SearchTeam", err)
			return
		}
		for _, t := range teams {
			results = append(results, ascAccessResult{Kind: ascAccessTeam, Name: t.Name, Description: t.Description})
		}
	}
	// the exact name first, as the add form takes it as typed
	lower := strings.ToLower(q)
	slices.SortStableFunc(results, func(a, b ascAccessResult) int {
		ae, be := strings.ToLower(a.Name) == lower, strings.ToLower(b.Name) == lower
		switch {
		case ae == be:
			return 0
		case ae:
			return -1
		default:
			return 1
		}
	})
	ctx.JSON(http.StatusOK, map[string]any{"data": results})
}

// ascSearchesTeams reports whether the search may list the teams of the owner: the owner is an
// organization, the doer may add a team (as in ascAddTeam) and is a member of the organization,
// whose teams a member can see anyway.
func ascSearchesTeams(ctx *context.Context) bool {
	if !ctx.Repo.Owner.IsOrganization() {
		return false
	}
	if !ctx.Repo.Owner.RepoAdminChangeTeamAccess && !ctx.Repo.IsOwner() {
		return false
	}
	if ctx.Doer.IsAdmin {
		return true
	}
	member, err := organization.IsOrganizationMember(ctx, ctx.Repo.Owner.ID, ctx.Doer.ID)
	return err == nil && member
}
