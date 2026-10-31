// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package org

import (
	"errors"

	org_model "forgejo.org/models/organization"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/log"
	"forgejo.org/services/context"
	org_service "forgejo.org/services/org"
)

// transferCandidates are the members of the organization who are not owners yet: the people ownership can pass to.
func transferCandidates(ctx *context.Context) []*user_model.User {
	members, _, err := ctx.Org.Organization.GetMembers(ctx, ctx.Doer)
	if err != nil {
		log.Error("transfer candidates of %s: %v", ctx.Org.Organization.Name, err)
		return nil
	}
	var out []*user_model.User
	for _, m := range members {
		if owned, err := ctx.Org.Organization.IsOwnedBy(ctx, m.ID); err == nil && !owned {
			out = append(out, m)
		}
	}
	return out
}

// SettingsTransferOwnership serves `POST /{org}/settings/transfer_owner` (owners only, as the whole settings area):
// fields new_owner (a login), org_name (the name of the organization, typed to confirm) and leave (also leave the Owners team).
func SettingsTransferOwnership(ctx *context.Context) {
	back := ctx.Org.OrgLink + "/settings/delete"
	if ctx.Org.Organization.Name != ctx.FormString("org_name") {
		ctx.Flash.Error(ctx.Tr("form.enterred_invalid_org_name"))
		ctx.Redirect(back)
		return
	}
	target, err := user_model.GetUserByName(ctx, ctx.FormString("new_owner"))
	if err != nil || target.IsOrganization() {
		ctx.Flash.Error(ctx.Tr("org.settings.transfer_no_such_user"))
		ctx.Redirect(back)
		return
	}
	leave := ctx.FormBool("leave")
	switch err := org_service.TransferOwnership(ctx, ctx.Org.Organization, ctx.Doer.ID, target.ID, leave); {
	case err == nil:
		log.Info("Organization %s: ownership passed to %s by %s (leaving: %v)", ctx.Org.Organization.Name, target.Name, ctx.Doer.Name, leave)
		ctx.Flash.Success(ctx.Tr("org.settings.transfer_done", target.Name))
		if leave {
			ctx.Redirect(ctx.Org.Organization.HomeLink())
			return
		}
	case errors.Is(err, org_service.ErrTransferNotMember):
		ctx.Flash.Error(ctx.Tr("org.settings.transfer_not_member"))
	case errors.Is(err, org_service.ErrTransferAlreadyOwner):
		ctx.Flash.Error(ctx.Tr("org.settings.transfer_already_owner"))
	case errors.Is(err, org_service.ErrTransferNotOwner):
		ctx.Error(403)
		return
	case org_model.IsErrLastOrgOwner(err):
		ctx.Flash.Error(ctx.Tr("org.settings.transfer_last_owner"))
	default:
		ctx.ServerError("TransferOwnership", err)
		return
	}
	ctx.Redirect(back)
}
