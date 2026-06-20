// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package permissions

import (
	"forgejo.org/models/unit"
	"forgejo.org/modules/log"
)

func MustAllowPulls(ctx Context) {
	if !ctx.GetRepository().CanEnablePulls() || !ctx.GetPermission().CanRead(unit.TypePullRequests) {
		if ctx.GetRepository().CanEnablePulls() && log.IsTrace() {
			if ctx.GetIsSigned() {
				log.Trace("Permission Denied: User %-v cannot read %-v in Repo %-v\n"+
					"User in Repo has Permissions: %-+v",
					ctx.GetDoer(),
					unit.TypePullRequests,
					ctx.GetRepository(),
					ctx.GetPermission())
			} else {
				log.Trace("Permission Denied: Anonymous user cannot read %-v in Repo %-v\n"+
					"Anonymous user in Repo has Permissions: %-+v",
					unit.TypePullRequests,
					ctx.GetRepository(),
					ctx.GetPermission())
			}
		}
		ctx.NotFound()
		return
	}
}
