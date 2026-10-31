// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package setting

import (
	"forgejo.org/models/organization"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/log"
	"forgejo.org/modules/setting"
	"forgejo.org/services/context"
)

// OrgInviteRow is one invitation on the organizations page: who asked, for which team of which organization.
type OrgInviteRow struct {
	Token   string
	Org     *organization.Organization
	Team    *organization.Team
	Inviter *user_model.User
	Accept  string // the address of the invitation page, where the invitation is accepted
}

// orgInviteRows reads the pending invitations of the person; one that cannot be read (its team or organization is gone) is skipped.
func orgInviteRows(ctx *context.Context) []OrgInviteRow {
	invites, err := organization.GetPendingInvitesForUser(ctx, ctx.Doer)
	if err != nil {
		log.Error("pending invitations of user %d: %v", ctx.Doer.ID, err)
		return nil
	}
	rows := make([]OrgInviteRow, 0, len(invites))
	for _, inv := range invites {
		team, err := organization.GetTeamByID(ctx, inv.TeamID)
		if err != nil {
			continue
		}
		o, err := organization.GetOrgByID(ctx, team.OrgID)
		if err != nil {
			continue
		}
		inviter, err := user_model.GetUserByID(ctx, inv.InviterID)
		if err != nil {
			inviter = nil
		}
		rows = append(rows, OrgInviteRow{Token: inv.Token, Org: o, Team: team, Inviter: inviter, Accept: setting.AppSubURL + "/org/invite/" + inv.Token})
	}
	return rows
}

// DeclineOrgInvite serves `POST /user/settings/organization/decline` with the field token.
func DeclineOrgInvite(ctx *context.Context) {
	err := organization.DeclineInviteForUser(ctx, ctx.Doer, ctx.FormString("token"))
	switch {
	case err == nil:
		ctx.Flash.Success(ctx.Tr("settings.org_invite_declined"))
	case organization.IsErrTeamInviteNotFound(err):
		ctx.Flash.Error(ctx.Tr("settings.org_invite_gone"))
	default:
		ctx.ServerError("DeclineInviteForUser", err)
		return
	}
	ctx.Redirect(setting.AppSubURL + "/user/settings/organization")
}
