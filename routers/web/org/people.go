// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package org

import (
	gocontext "context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"forgejo.org/models"
	"forgejo.org/models/db"
	"forgejo.org/models/organization"
	"forgejo.org/models/perm"
	repo_model "forgejo.org/models/repo"
	user_model "forgejo.org/models/user"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/log"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/timeutil"
	"forgejo.org/services/asctrust"
	"forgejo.org/services/context"
	"forgejo.org/services/mailer"
	org_service "forgejo.org/services/org"
)

// The People page (board OrgPeople). A person's role is read from the teams they belong to: a member of the owners team is
// an Owner, a member of a team with the administrator permission is a Maintainer, everyone else is a Member. Changing a role
// moves the person between those teams; nothing else about the organisation's teams changes.

const (
	roleOwner      = "owner"
	roleMaintainer = "maintainer"
	roleMember     = "member"

	// maintainersTeamName is the team a person is put into when they are made a Maintainer and no team with the administrator
	// permission exists yet.
	maintainersTeamName = "Maintainers"
)

func isAdminTeam(t *organization.Team) bool {
	return !t.IsOwnerTeam() && t.AccessMode >= perm.AccessModeAdmin
}

// roleOfTeams is the role of a person in the teams they belong to.
func roleOfTeams(teams []*organization.Team) string {
	role := roleMember
	for _, t := range teams {
		if t.IsOwnerTeam() {
			return roleOwner
		}
		if isAdminTeam(t) {
			role = roleMaintainer
		}
	}
	return role
}

// teamForRole finds the team that stands for a role: the owners team, or the team with the administrator permission
// (the one called Maintainers when there are several). With create, a missing Maintainers team is made.
func teamForRole(ctx gocontext.Context, org *organization.Organization, role string, create bool) (*organization.Team, error) {
	if role == roleOwner {
		return org.GetOwnerTeam(ctx)
	}
	teams, err := organization.FindOrgTeams(ctx, org.ID)
	if err != nil {
		return nil, err
	}
	var found *organization.Team
	for _, t := range teams {
		if !isAdminTeam(t) {
			continue
		}
		if found == nil || strings.EqualFold(t.Name, maintainersTeamName) {
			found = t
		}
	}
	if found != nil || !create {
		return found, nil
	}
	t := &organization.Team{OrgID: org.ID, Name: maintainersTeamName, AccessMode: perm.AccessModeAdmin, IncludesAllRepositories: true}
	if err := models.NewTeam(ctx, t); err != nil {
		return nil, err
	}
	return t, nil
}

var (
	errNoRoleTeam = errors.New("no team to put the person in")
)

// setRole puts a person in the role. The person is added to the team of the new role before they are taken out of the
// teams of the old one, so they never leave the organisation on the way. Teams that are neither the owners team nor have the
// administrator permission are not touched, and a person who would be left in none gets the first of them (or the Members
// default) to stay in the organisation.
func setRole(ctx gocontext.Context, org *organization.Organization, uid int64, role string) error {
	teams, err := organization.GetUserOrgTeams(ctx, org.ID, uid)
	if err != nil {
		return err
	}
	if roleOfTeams(teams) == role {
		return nil
	}
	switch role {
	case roleOwner, roleMaintainer:
		t, err := teamForRole(ctx, org, role, true)
		if err != nil {
			return err
		}
		if !slices.ContainsFunc(teams, func(x *organization.Team) bool { return x.ID == t.ID }) {
			if err := models.AddTeamMember(ctx, t, uid); err != nil {
				return err
			}
		}
		// an owner is not also a separate maintainer: the owners team is the higher role
		for _, x := range teams {
			if x.ID != t.ID && (role == roleOwner && isAdminTeam(x) || role == roleMaintainer && x.IsOwnerTeam()) {
				if err := models.RemoveTeamMember(ctx, x, uid); err != nil {
					return err
				}
			}
		}
		return nil
	default: // member
		var keep *organization.Team
		for _, x := range teams {
			if !x.IsOwnerTeam() && !isAdminTeam(x) {
				keep = x
				break
			}
		}
		if keep == nil {
			all, err := organization.FindOrgTeams(ctx, org.ID)
			if err != nil {
				return err
			}
			for _, x := range all {
				if !x.IsOwnerTeam() && !isAdminTeam(x) {
					keep = x
					break
				}
			}
			if keep == nil {
				return errNoRoleTeam
			}
			if err := models.AddTeamMember(ctx, keep, uid); err != nil {
				return err
			}
		}
		for _, x := range teams {
			if x.IsOwnerTeam() || isAdminTeam(x) {
				if err := models.RemoveTeamMember(ctx, x, uid); err != nil {
					return err
				}
			}
		}
		return nil
	}
}

// teamNamesOf lists the teams of a person for the row, without the owners team (that is the Owner chip).
func teamNamesOf(teams []*organization.Team) string {
	var names []string
	for _, t := range teams {
		if !t.IsOwnerTeam() {
			names = append(names, t.Name)
		}
	}
	return strings.Join(names, ", ")
}

// ---------------------------------------------------------------------------------------------- pending invitations

// pendingInvite is one invitation that has not been answered. A person invited to several teams is one row.
type pendingInvite struct {
	ID      int64 // an invitation of the person (Resend and Revoke act on it, and Revoke on the person's others)
	TeamID  int64
	Email   string
	User    *user_model.User
	Role    string
	By      *user_model.User
	Created timeutil.TimeStamp
}

func (p *pendingInvite) key() string {
	if p.User != nil {
		return "u:" + p.User.LowerName
	}
	return "e:" + strings.ToLower(p.Email)
}

func rank(role string) int {
	switch role {
	case roleOwner:
		return 2
	case roleMaintainer:
		return 1
	}
	return 0
}

// pendingInvites lists the invitations of the organisation's teams, one row per person.
func pendingInvites(ctx gocontext.Context, teams []*organization.Team) ([]*pendingInvite, error) {
	rows := map[string]*pendingInvite{}
	var order []string
	for _, t := range teams {
		invites, err := organization.GetInvitesByTeamID(ctx, t.ID)
		if err != nil {
			return nil, err
		}
		role := roleMember
		if t.IsOwnerTeam() {
			role = roleOwner
		} else if isAdminTeam(t) {
			role = roleMaintainer
		}
		for _, in := range invites {
			p := &pendingInvite{ID: in.ID, TeamID: in.TeamID, Email: in.Email, Role: role, Created: in.CreatedUnix}
			if err := in.LoadInvitedUser(ctx); err == nil {
				p.User = in.InvitedUser
			}
			if by, err := user_model.GetUserByID(ctx, in.InviterID); err == nil {
				p.By = by
			}
			k := p.key()
			if old, ok := rows[k]; ok {
				if rank(p.Role) > rank(old.Role) {
					old.Role = p.Role
				}
				continue
			}
			rows[k] = p
			order = append(order, k)
		}
	}
	out := make([]*pendingInvite, 0, len(order))
	for _, k := range order {
		out = append(out, rows[k])
	}
	slices.SortFunc(out, func(a, b *pendingInvite) int { return int(b.Created) - int(a.Created) })
	return out, nil
}

// inviteByID finds an invitation of the organisation and the team it is for.
func inviteByID(ctx gocontext.Context, org *organization.Organization, id int64) (*organization.TeamInvite, *organization.Team, error) {
	teams, err := organization.FindOrgTeams(ctx, org.ID)
	if err != nil {
		return nil, nil, err
	}
	for _, t := range teams {
		invites, err := organization.GetInvitesByTeamID(ctx, t.ID)
		if err != nil {
			return nil, nil, err
		}
		for _, in := range invites {
			if in.ID == id {
				return in, t, nil
			}
		}
	}
	return nil, nil, organization.ErrTeamInviteNotFound{}
}

// revokeInvite removes the invitation and the other invitations of the same person to the organisation's teams.
func revokeInvite(ctx gocontext.Context, org *organization.Organization, id int64) error {
	in, _, err := inviteByID(ctx, org, id)
	if err != nil {
		return err
	}
	teams, err := organization.FindOrgTeams(ctx, org.ID)
	if err != nil {
		return err
	}
	for _, t := range teams {
		invites, err := organization.GetInvitesByTeamID(ctx, t.ID)
		if err != nil {
			return err
		}
		for _, other := range invites {
			same := other.ID == in.ID
			if !same {
				aok, a := in.InvitedID.Get()
				bok, b := other.InvitedID.Get()
				same = aok && bok && a == b || !aok && !bok && strings.EqualFold(in.Email, other.Email)
			}
			if same {
				if err := organization.RemoveInviteByID(ctx, other.ID, t.ID); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// resendInvite sends the mail of an invitation again.
func resendInvite(ctx *context.Context, org *organization.Organization, id int64) error {
	in, t, err := inviteByID(ctx, org, id)
	if err != nil {
		return err
	}
	return mailer.MailTeamInvite(ctx, ctx.Doer, t, in)
}

// ---------------------------------------------------------------------------------------------- invite many

// inviteeWords splits what was typed in the invite dialog: names and addresses separated by commas, spaces or lines.
func inviteeWords(s string) []string {
	var out []string
	seen := map[string]bool{}
	for _, w := range strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == ';' || r == ' ' || r == '\n' || r == '\r' || r == '\t' }) {
		k := strings.ToLower(w)
		if w != "" && !seen[k] {
			seen[k] = true
			out = append(out, w)
		}
	}
	return out
}

// inviteMany invites or adds each person to the team of the role (and to the extra teams ticked). It returns the words that
// could not be used, each with the reason, so the page can say which ones were left out.
func inviteMany(ctx *context.Context, org *organization.Organization, words []string, role string, extra []*organization.Team) (done int, problems []string, err error) {
	teams := slices.Clone(extra)
	switch role {
	case roleOwner, roleMaintainer:
		t, e := teamForRole(ctx, org, role, true)
		if e != nil {
			return 0, nil, e
		}
		teams = append(teams, t)
	}
	if len(teams) == 0 {
		all, e := organization.FindOrgTeams(ctx, org.ID)
		if e != nil {
			return 0, nil, e
		}
		for _, t := range all {
			if !t.IsOwnerTeam() && !isAdminTeam(t) {
				teams = append(teams, t)
				break
			}
		}
	}
	if len(teams) == 0 {
		return 0, nil, errNoRoleTeam
	}
	for _, w := range words {
		if strings.Contains(w, "@") {
			if setting.MailService == nil {
				problems = append(problems, w+": "+ctx.Locale.TrString("asc_work.invite_no_mail"))
				continue
			}
			ok := false
			for _, t := range teams {
				if e := org_service.CreateTeamInviteByEmail(ctx, ctx.Doer, t, w); e != nil {
					if organization.IsErrTeamInviteAlreadyExist(e) {
						ok = true
						continue
					}
					problems = append(problems, w+": "+e.Error())
					ok = false
					break
				}
				ok = true
			}
			if ok {
				done++
			}
			continue
		}
		u, e := user_model.GetUserByName(ctx, w)
		if e != nil || u.IsOrganization() {
			problems = append(problems, w+": "+ctx.Locale.TrString("asc_work.invite_unknown"))
			continue
		}
		if member, e := org.IsOrgMember(ctx, u.ID); e == nil && member {
			problems = append(problems, w+": "+ctx.Locale.TrString("members.user_already_member"))
			continue
		}
		ok := true
		for _, t := range teams {
			if e := org_service.InviteOrAddTeamMember(ctx, ctx.Doer, u, t); e != nil && !organization.IsErrTeamInviteAlreadyExist(e) {
				problems = append(problems, w+": "+e.Error())
				ok = false
				break
			}
		}
		if ok {
			done++
		}
	}
	return done, problems, nil
}

// ---------------------------------------------------------------------------------------------- numbers from asc

// peopleCache is what asc said about the changes each person landed in the organisation, and when.
type peopleCache struct {
	at     time.Time
	landed map[string]int
}

var (
	peopleMu       sync.Mutex
	peopleCacheMap = map[[2]int64]peopleCache{}
)

const (
	peopleCacheTTL = time.Minute
	// peopleMaxRepos bounds the repositories asked about: each answer is an expensive read in asc.
	peopleMaxRepos = 24
)

// landedByPerson sums Insights.ListContributors of the organisation's asc repositories, as the viewer, by login. It returns
// nil when asc has nothing to say (not enabled, no repository, every call failed). The numbers are cached for a minute.
func landedByPerson(ctx *context.Context, org *organization.Organization) map[string]int {
	if !setting.Asc.Enabled || ctx.Doer == nil {
		return nil
	}
	key := [2]int64{org.ID, ctx.Doer.ID}
	peopleMu.Lock()
	if c, ok := peopleCacheMap[key]; ok && time.Since(c.at) < peopleCacheTTL {
		peopleMu.Unlock()
		return c.landed
	}
	peopleMu.Unlock()

	repos, _, err := repo_model.SearchRepository(ctx, &repo_model.SearchRepoOptions{
		ListOptions: db.ListOptions{PageSize: peopleMaxRepos, Page: 1},
		OwnerID:     org.ID, Private: true, Actor: ctx.Doer,
	})
	if err != nil {
		return nil
	}
	conn, err := asctrust.Conn()
	if err != nil {
		return nil
	}
	out := map[string]int{}
	asked := 0
	for _, repo := range repos {
		if repo.GetStorageBackend() != "asc" || !repo.CanEnablePulls() {
			continue
		}
		name, err := asctrust.RepoName(repo)
		if err != nil {
			continue
		}
		callCtx, release, err := asctrust.UserCall(ctx, ctx.Doer, asctrust.CallRepoRead, repo)
		if err != nil {
			continue
		}
		cctx, cancel := gocontext.WithTimeout(callCtx, asctrust.CallTimeout)
		resp, err := ascv1.NewInsightsClient(conn).ListContributors(cctx, &ascv1.ListContributorsRequest{Repo: asctrust.RepoRef(name), Days: 366, Kind: ascv1.AuthorKind_AUTHOR_KIND_PERSON})
		cancel()
		release()
		if err != nil {
			log.Debug("people: asc contributors of %s: %v", repo.FullName(), err)
			continue
		}
		asked++
		for _, c := range resp.GetContributors() {
			out[strings.ToLower(c.GetName())] += int(c.GetLanded())
		}
	}
	if asked == 0 {
		out = nil
	}
	peopleMu.Lock()
	peopleCacheMap[key] = peopleCache{at: time.Now(), landed: out}
	peopleMu.Unlock()
	return out
}

// orgAgents lists the agents of the organisation for the People page. Nothing is shown when asc cannot say, or the viewer
// may not read the organisation's agents there.
func orgAgents(ctx *context.Context, org *organization.Organization) []asctrust.OrgAgent {
	if !setting.Asc.Enabled || ctx.Doer == nil || !asctrust.HasFeature(ctx, asctrust.FeatureOrgSettings) {
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
	agents, err := asctrust.ListOrgAgents(callCtx, conn, org.Name)
	if err != nil {
		log.Debug("people: asc agents of %s: %v", org.Name, err)
		return nil
	}
	return agents
}

// validRole is the roles the change-role action accepts.
func validRole(role string) bool {
	return role == roleOwner || role == roleMaintainer || role == roleMember
}

// changeRole serves the role action: an owner changes the role of another member.
func changeRole(ctx *context.Context, org *organization.Organization, uid int64, role string) {
	if !ctx.Org.IsOwner || !validRole(role) {
		ctx.Error(http.StatusNotFound)
		return
	}
	back := ctx.Org.OrgLink + "/members"
	if member, err := org.IsOrgMember(ctx, uid); err != nil || !member {
		ctx.Error(http.StatusNotFound)
		return
	}
	if err := setRole(ctx, org, uid, role); err != nil {
		switch {
		case organization.IsErrLastOrgOwner(err):
			ctx.Flash.Error(ctx.Tr("form.last_org_owner"))
		case errors.Is(err, errNoRoleTeam):
			ctx.Flash.Error(ctx.Tr("asc_work.role_no_team"))
		default:
			ctx.ServerError("setRole", err)
			return
		}
		ctx.JSONRedirect(back)
		return
	}
	ctx.JSONRedirect(back)
}

// inviteSeveral serves the add action of the invite dialog: invitees (names and addresses), a role and, optionally, more teams.
func inviteSeveral(ctx *context.Context) {
	back := ctx.Org.OrgLink + "/members"
	if !ctx.Org.IsOwner {
		ctx.Error(http.StatusNotFound)
		return
	}
	role := ctx.FormString("role")
	if !validRole(role) {
		role = roleMember
	}
	words := inviteeWords(ctx.FormString("invitees"))
	if len(words) == 0 || len(words) > 50 {
		ctx.Flash.Error(ctx.Tr("asc_work.invite_none"))
		ctx.Redirect(back)
		return
	}
	teams, err := organization.FindOrgTeams(ctx, ctx.Org.Organization.ID)
	if err != nil {
		ctx.ServerError("FindOrgTeams", err)
		return
	}
	var extra []*organization.Team
	for _, t := range teams {
		if ctx.FormBool(fmt.Sprintf("team_%d", t.ID)) {
			extra = append(extra, t)
		}
	}
	done, problems, err := inviteMany(ctx, ctx.Org.Organization, words, role, extra)
	switch {
	case errors.Is(err, errNoRoleTeam):
		ctx.Flash.Error(ctx.Tr("asc_work.role_no_team"))
	case err != nil:
		ctx.ServerError("inviteMany", err)
		return
	default:
		if done > 0 {
			ctx.Flash.Success(ctx.Tr("asc_work.invited_n", done))
		}
		if len(problems) > 0 {
			ctx.Flash.Error(strings.Join(problems, "; "))
		}
	}
	if setting.Service.AddMembersByInvitations {
		back += "?filter=pending"
	}
	ctx.Redirect(back)
}
