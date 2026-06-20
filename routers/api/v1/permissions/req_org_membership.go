// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package permissions

import (
	"net/http"

	"forgejo.org/models/organization"
)

func ReqOrgMembership(ctx Context) {
	if IsUserSiteAdmin(ctx) {
		return
	}

	var orgID int64
	if ctx.GetOrg() != nil {
		orgID = ctx.GetOrg().ID
	} else if ctx.GetTeam() != nil {
		orgID = ctx.GetTeam().OrgID
	} else {
		ctx.Error(http.StatusInternalServerError, "", "reqOrgMembership: unprepared context")
		return
	}

	if isMember, err := organization.IsOrganizationMember(ctx.GetContext(), orgID, ctx.GetDoer().ID); err != nil {
		ctx.Error(http.StatusInternalServerError, "IsOrganizationMember", err)
		return
	} else if !isMember {
		if ctx.GetOrg() != nil {
			ctx.Error(http.StatusForbidden, "", "Must be an organization member")
		} else {
			ctx.NotFound()
		}
		return
	}
}
