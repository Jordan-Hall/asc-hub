// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package permissions

import (
	"net/http"

	"forgejo.org/models/organization"
)

func ReqOrgOwnership(ctx Context) {
	if IsUserSiteAdmin(ctx) {
		return
	}

	var orgID int64
	if ctx.GetOrg() != nil {
		orgID = ctx.GetOrg().ID
	} else if ctx.GetTeam() != nil {
		orgID = ctx.GetTeam().OrgID
	} else {
		ctx.Error(http.StatusInternalServerError, "", "reqOrgOwnership: unprepared context")
		return
	}

	isOwner, err := organization.IsOrganizationOwner(ctx.GetContext(), orgID, ctx.GetDoer().ID)
	if err != nil {
		ctx.Error(http.StatusInternalServerError, "IsOrganizationOwner", err)
		return
	} else if !isOwner {
		if ctx.GetOrg() != nil {
			ctx.Error(http.StatusForbidden, "", "Must be an organization owner")
		} else {
			ctx.NotFound()
		}
		return
	}
}
