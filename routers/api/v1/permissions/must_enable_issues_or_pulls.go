// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package permissions

import (
	"forgejo.org/models/unit"
	"forgejo.org/modules/log"
)

func MustEnableIssuesOrPulls(ctx Context) {
	if !ctx.GetPermission().CanRead(unit.TypeIssues) &&
		(!ctx.GetRepository().CanEnablePulls() || !ctx.GetPermission().CanRead(unit.TypePullRequests)) {
		if ctx.GetRepository().CanEnablePulls() && log.IsTrace() {
			if ctx.GetIsSigned() {
				log.Trace("Permission Denied: User %-v cannot read %-v and %-v in Repo %-v\n"+
					"User in Repo has Permissions: %-+v",
					ctx.GetDoer(),
					unit.TypeIssues,
					unit.TypePullRequests,
					ctx.GetRepository(),
					ctx.GetPermission())
			} else {
				log.Trace("Permission Denied: Anonymous user cannot read %-v and %-v in Repo %-v\n"+
					"Anonymous user in Repo has Permissions: %-+v",
					unit.TypeIssues,
					unit.TypePullRequests,
					ctx.GetRepository(),
					ctx.GetPermission())
			}
		}
		ctx.NotFound()
	}
}
