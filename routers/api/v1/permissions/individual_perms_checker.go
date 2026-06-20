// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package permissions

import (
	api "forgejo.org/modules/structs"
)

func IndividualPermsChecker(ctx Context) {
	// org permissions have been checked in context.OrgAssignment(), but individual permissions haven't been checked.
	if ctx.GetUser().IsIndividual() {
		switch ctx.GetUser().Visibility {
		case api.VisibleTypePrivate:
			if ctx.GetDoer() == nil || (ctx.GetUser().ID != ctx.GetDoer().ID && !IsUserSiteAdmin(ctx)) {
				ctx.NotFound("Visit Project", nil)
				return
			}
		case api.VisibleTypeLimited:
			if ctx.GetDoer() == nil {
				ctx.NotFound("Visit Project", nil)
				return
			}
		}
	}
}
