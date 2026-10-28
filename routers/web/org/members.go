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

	pager := context.NewPagination(len(members), setting.UI.MembersPagingNum, page, 5)
	pager.AddParam(ctx, "q", "PeopleQuery")
	pager.AddParam(ctx, "filter", "PeopleFilter")
	if from := (page - 1) * setting.UI.MembersPagingNum; from < len(members) {
		members = members[from:min(from+setting.UI.MembersPagingNum, len(members))]
	} else {
		members = nil
	}
	teams, err := organization.FindOrgTeams(ctx, org.ID)
	if err != nil {
		ctx.ServerError("GetTeams", err)
		return
	}
	ctx.Data["Page"] = pager
	ctx.Data["Members"] = members
	ctx.Data["Teams"] = teams
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
