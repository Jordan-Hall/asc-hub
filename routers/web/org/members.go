// Copyright 2014 The Gogs Authors. All rights reserved.
// Copyright 2020 The Gitea Authors.
// SPDX-License-Identifier: MIT

package org

import (
	"fmt"
	"net/http"
	"strings"

	"forgejo.org/models"
	"forgejo.org/models/organization"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/base"
	"forgejo.org/modules/log"
	"forgejo.org/modules/setting"
	shared_user "forgejo.org/routers/web/shared/user"
	"forgejo.org/services/context"
	org_service "forgejo.org/services/org"
)

const (
	// tplMembers template for organization members page
	tplMembers base.TplName = "org/member/members"
)

// Members render organization users page
func Members(ctx *context.Context) {
	org := ctx.Org.Organization
	ctx.Data["Title"] = org.FullName
	ctx.Data["PageIsOrgMembers"] = true

	page := max(ctx.FormInt("page"), 1)

	opts := &organization.FindOrgMembersOpts{
		Doer:  ctx.Doer,
		OrgID: org.ID,
	}

	if ctx.Doer != nil {
		isMember, err := ctx.Org.Organization.IsOrgMember(ctx, ctx.Doer.ID)
		if err != nil {
			ctx.Error(http.StatusInternalServerError, "IsOrgMember")
			return
		}
		opts.IsDoerMember = isMember
	}
	ctx.Data["PublicOnly"] = opts.PublicOnly()

	_, err := organization.CountOrgMembers(ctx, opts)
	if err != nil {
		ctx.Error(http.StatusInternalServerError, "CountOrgMembers")
		return
	}

	err = shared_user.LoadHeaderCount(ctx)
	if err != nil {
		ctx.ServerError("LoadHeaderCount", err)
		return
	}

	// People page (board OrgPeople): the whole list is read, filtered by the search text and the chosen chip, then
	// paged, so the chip counts and the pager agree with what is shown.
	everyone, membersIsPublic, err := organization.FindOrgMembers(ctx, &organization.FindOrgMembersOpts{Doer: opts.Doer, IsDoerMember: opts.IsDoerMember, OrgID: org.ID})
	if err != nil {
		ctx.ServerError("GetMembers", err)
		return
	}
	isOwner := organization.IsUserOrgOwner(ctx, everyone, org.ID)
	// 2FA status of members is for owners and site administrators only: the status, its count and the filter
	canSee2FA := ctx.Org.IsOwner && !opts.PublicOnly()
	twoFA := map[int64]bool{}
	if canSee2FA {
		twoFA = everyone.GetTwoFaStatus(ctx)
	}
	query := strings.ToLower(ctx.FormTrim("q"))
	filter := ctx.FormString("filter")
	if filter == "2fa_off" && !canSee2FA {
		filter = ""
	}
	if filter == "pending" && !ctx.Org.IsOwner {
		filter = ""
	}
	var ownersCount, twoFAOffCount int
	members := make(user_model.UserList, 0, len(everyone))
	for _, m := range everyone {
		if isOwner[m.ID] {
			ownersCount++
		}
		if canSee2FA && !twoFA[m.ID] {
			twoFAOffCount++
		}
		if query != "" && !strings.Contains(strings.ToLower(m.Name), query) && !strings.Contains(strings.ToLower(m.FullName), query) {
			continue
		}
		if (filter == "owners" && !isOwner[m.ID]) || (filter == "2fa_off" && twoFA[m.ID]) {
			continue
		}
		members = append(members, m)
	}
	ctx.Data["PeopleQuery"] = ctx.FormTrim("q")
	ctx.Data["PeopleFilter"] = filter
	ctx.Data["PeopleCountAll"] = len(everyone)
	ctx.Data["PeopleCountOwners"] = ownersCount
	ctx.Data["CanSee2FA"] = canSee2FA
	if canSee2FA {
		ctx.Data["PeopleCountTwoFAOff"] = twoFAOffCount
	}
	ctx.Data["PeopleShown"] = len(members)

	teams, err := organization.FindOrgTeams(ctx, org.ID)
	if err != nil {
		ctx.ServerError("GetTeams", err)
		return
	}
	// the role of each person, read from their teams, and the teams for the second line of the row. Teams and roles are for the
	// members of the organization: a visitor who is not one sees the people the organization made public and nothing of its
	// teams (the names of private teams and who administers them are not public). The owners come first, as on the board.
	if !opts.PublicOnly() {
		roles, teamNames, err := rolesAndTeams(ctx, org, teams)
		if err != nil {
			ctx.ServerError("rolesAndTeams", err)
			return
		}
		sortByRole(members, roles)
		ctx.Data["MembersRole"] = roles
		ctx.Data["MembersTeams"] = teamNames
	}

	pager := context.NewPagination(len(members), setting.UI.MembersPagingNum, page, 5)
	pager.AddParam(ctx, "q", "PeopleQuery")
	pager.AddParam(ctx, "filter", "PeopleFilter")
	if from := (page - 1) * setting.UI.MembersPagingNum; from < len(members) {
		members = members[from:min(from+setting.UI.MembersPagingNum, len(members))]
	} else {
		members = nil
	}
	if ctx.Org.IsOwner {
		ctx.Data["RoleUndo"] = roleUndoOf(ctx, org, ctx.FormString("undo"))
		ctx.Data["RoleChoices"] = []string{roleOwner, roleMaintainer, roleMember}
	}
	// what asc knows: the changes each person landed in the last year, and the agents of the organisation
	if !opts.PublicOnly() {
		if landed := landedByPerson(ctx, org); landed != nil {
			ctx.Data["MembersLanded"] = landed
		}
	}
	if !opts.PublicOnly() {
		ctx.Data["OrgAgents"] = orgAgents(ctx, org)
	}
	// invitations that have not been answered: for the owners, who can resend and revoke them
	if ctx.Org.IsOwner {
		invites, err := pendingInvites(ctx, teams)
		if err != nil {
			ctx.ServerError("pendingInvites", err)
			return
		}
		ctx.Data["PendingInvites"] = invites
		ctx.Data["PeopleCountPending"] = len(invites)
		ctx.Data["ShowPending"] = len(invites) > 0 || setting.Service.AddMembersByInvitations
	}
	ctx.Data["Page"] = pager
	ctx.Data["Members"] = members
	if ctx.Org.IsOwner { // the invite dialog is the owners'
		ctx.Data["Teams"] = teams
	}
	ctx.Data["MembersIsPublicMember"] = membersIsPublic
	ctx.Data["MembersIsUserOrgOwner"] = isOwner
	if canSee2FA {
		ctx.Data["MembersTwoFaStatus"] = twoFA
	}
	ctx.Data["AddMembersByInvitations"] = setting.Service.AddMembersByInvitations

	ctx.HTML(http.StatusOK, tplMembers)
}

// MembersAction response for operation to a member of organization
func MembersAction(ctx *context.Context) {
	// the invitations that have not been answered: resend the mail, or take the invitation back (owners only)
	if a := ctx.Params(":action"); a == "resend_invite" || a == "revoke_invite" {
		if !ctx.Org.IsOwner {
			ctx.Error(http.StatusNotFound)
			return
		}
		back := ctx.Org.OrgLink + "/members?filter=pending"
		var err error
		if a == "revoke_invite" {
			err = revokeInvite(ctx, ctx.Org.Organization, ctx.FormInt64("id"))
		} else {
			err = resendInvite(ctx, ctx.Org.Organization, ctx.FormInt64("id"))
		}
		switch {
		case organization.IsErrTeamInviteNotFound(err):
			ctx.Flash.Error(ctx.Tr("asc_work.invite_gone"))
		case err != nil:
			ctx.ServerError(a, err)
			return
		case a == "resend_invite":
			ctx.Flash.Success(ctx.Tr("asc_work.invite_resent"))
		default:
			ctx.Flash.Success(ctx.Tr("asc_work.invite_revoked"))
		}
		ctx.JSONRedirect(back)
		return
	}
	// the dialog of several people and a role: the words are checked one by one and the ones that cannot be used are named
	if ctx.Params(":action") == "add" && ctx.FormString("invitees") != "" {
		inviteSeveral(ctx)
		return
	}

	uid := ctx.FormInt64("uid")
	if uid == 0 {
		ctx.Redirect(ctx.Org.OrgLink + "/members")
		return
	}

	org := ctx.Org.Organization
	var err error
	switch ctx.Params(":action") {
	case "private":
		if ctx.Doer.ID != uid && !ctx.Org.IsOwner {
			ctx.Error(http.StatusNotFound)
			return
		}
		err = organization.ChangeOrgUserStatus(ctx, org.ID, uid, false)
	case "public":
		if ctx.Doer.ID != uid && !ctx.Org.IsOwner {
			ctx.Error(http.StatusNotFound)
			return
		}
		err = organization.ChangeOrgUserStatus(ctx, org.ID, uid, true)
	case "remove":
		if !ctx.Org.IsOwner {
			ctx.Error(http.StatusNotFound)
			return
		}
		err = models.RemoveOrgUser(ctx, org.ID, uid)
		if organization.IsErrLastOrgOwner(err) {
			ctx.Flash.Error(ctx.Tr("form.last_org_owner"))
			ctx.JSONRedirect(ctx.Org.OrgLink + "/members")
			return
		}
	case "add":
		if !ctx.Org.IsOwner {
			ctx.Error(http.StatusNotFound)
			return
		}
		uname := ctx.FormString("uname")
		var u *user_model.User
		u, err = user_model.GetUserByName(ctx, uname)
		if err != nil {
			ctx.ServerError("GetUserByName", err)
			return
		}

		if u.IsOrganization() {
			ctx.Flash.Error(ctx.Tr("form.cannot_add_org_to_team"))
			ctx.Redirect(ctx.Org.OrgLink + "/members")
			return
		}

		alreadyMember, err := ctx.Org.Organization.IsOrgMember(ctx, u.ID)
		if err != nil {
			ctx.ServerError("IsOrgMember", err)
			return
		}
		if alreadyMember {
			ctx.Flash.Error(ctx.Tr("members.user_already_member"))
			ctx.Redirect(ctx.Org.OrgLink + "/members")
			return
		}
		if setting.Service.AddMembersByInvitations {
			alreadyInvited, err := ctx.Org.Organization.IsInvitedToOrg(ctx, u.ID)
			if err != nil {
				ctx.ServerError("IsInvitedToOrg", err)
				return
			}
			if alreadyInvited {
				ctx.Flash.Error(ctx.Tr("members.user_already_invited_to_org"))
				ctx.Redirect(ctx.Org.OrgLink + "/members")
				return
			}
		}

		teams, err := organization.FindOrgTeams(ctx, org.ID)
		if err != nil {
			ctx.ServerError("GetTeams", err)
			return
		}
		addedToTeam := false
		for _, team := range teams {
			addToTeam := ctx.FormBool(fmt.Sprintf("team_%d", team.ID))
			if addToTeam {
				err = org_service.InviteOrAddTeamMember(ctx, ctx.Doer, u, team)
				if err != nil {
					ctx.ServerError("InviteOrAddTeamMember", err)
					return
				}
				addedToTeam = true
			}
		}

		if !addedToTeam {
			ctx.Flash.Error(ctx.Tr("members.no_team_selected"))
		}
		ctx.Redirect(ctx.Org.OrgLink + "/members")
		return
	case "role":
		changeRole(ctx, org, uid, ctx.FormString("role"))
		return
	case "leave":
		err = models.RemoveOrgUser(ctx, org.ID, ctx.Doer.ID)
		if err == nil {
			ctx.Flash.Success(ctx.Tr("form.organization_leave_success", org.DisplayName()))
			ctx.JSON(http.StatusOK, map[string]any{
				"redirect": "", // keep the user stay on current page, in case they want to do other operations.
			})
		} else if organization.IsErrLastOrgOwner(err) {
			ctx.Flash.Error(ctx.Tr("form.last_org_owner"))
			ctx.JSONRedirect(ctx.Org.OrgLink + "/members")
		} else {
			log.Error("RemoveOrgUser(%d,%d): %v", org.ID, ctx.Doer.ID, err)
		}
		return
	}

	if err != nil {
		log.Error("Action(%s): %v", ctx.Params(":action"), err)
		ctx.JSON(http.StatusOK, map[string]any{
			"ok":  false,
			"err": err.Error(),
		})
		return
	}

	redirect := ctx.Org.OrgLink + "/members"
	if ctx.Params(":action") == "leave" {
		redirect = setting.AppSubURL + "/"
	}

	ctx.JSONRedirect(redirect)
}
