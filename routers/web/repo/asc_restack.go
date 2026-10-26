// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	"forgejo.org/modules/log"
	"forgejo.org/services/context"
	pull_service "forgejo.org/services/pull"
)

// AscStackRestack handles POST /{owner}/{repo}/pulls/{index}/asc/restack: asc rebases the whole stack of the
// pull request onto the current target, and the heads of the pull requests move to the new versions. The
// service decides who may (the poster, an administrator, a writer where the pull request allows maintainer edits,
// or whoever may merge it) and refuses the whole call when one pull request of the stack is not theirs to update.
func AscStackRestack(ctx *context.Context) {
	issue, pr, ok := ascStackTarget(ctx)
	if !ok {
		return
	}
	res, err := pull_service.RestackAscStack(ctx, ctx.Doer, pr, pull_service.AscRestackOptions{})
	if err != nil {
		if res != nil && len(res.Failed) > 0 {
			// asc has rewritten the stack; some heads did not move. Say so, the page shows what moved.
			log.Error("asc restack of pull request %d: %v", pr.ID, err)
			ctx.Flash.Error(ctx.Tr("repo.pulls.asc_op.err.restack_partial", len(res.Failed)))
			ctx.Redirect(issue.Link())
			return
		}
		ascOpFlashFail(ctx, ascOpRestack, issue, err)
		return
	}
	if res.UpToDate {
		ctx.Flash.Info(ctx.Tr("repo.pulls.asc_op.restack_up_to_date"))
	} else {
		ctx.Flash.Success(ctx.Tr("repo.pulls.asc_op.restacked", len(res.Moved)))
	}
	ctx.Redirect(issue.Link())
}
