// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	"net/http"

	"forgejo.org/modules/log"
	"forgejo.org/services/context"
	pull_service "forgejo.org/services/pull"
)

// ascRevertScope reads the "scope" field of the revert form: "stack" (the default) reverts down to the bottom of
// the landed stack, "this" only the pull request itself. ok is false for any other value.
func ascRevertScope(ctx *context.Context) (this, ok bool) {
	switch ctx.FormString("scope") {
	case "", "stack":
		return false, true
	case "this":
		return true, true
	}
	return false, false
}

// AscStackRevert handles POST /{owner}/{repo}/pulls/{index}/asc/revert: asc stores the reverts of a landed stack
// as new changes (it never writes the target branch) and one pull request per revert is opened, stacked on
// the one below. Merging them is an ordinary merge.
func AscStackRevert(ctx *context.Context) {
	issue, pr, ok := ascStackTarget(ctx)
	if !ok {
		return
	}
	this, ok := ascRevertScope(ctx)
	if !ok {
		ctx.Error(http.StatusBadRequest)
		return
	}
	opt := pull_service.AscRevertOptions{Squash: ctx.FormBool("squash")}
	if this {
		opt.Bottom = pr
	}
	res, err := pull_service.RevertAscStack(ctx, ctx.Doer, ctx.Repo.Permission, pr, opt)
	if err != nil {
		ascOpFlashFail(ctx, ascOpRevert, issue, err)
		return
	}
	if res.Err != nil {
		log.Error("asc revert of pull request %d: %v", pr.ID, res.Err)
		ctx.Flash.Error(ctx.Tr("repo.pulls.asc_op.err.revert_partial", len(res.Created)))
		ctx.Redirect(issue.Link())
		return
	}
	top := res.Created[len(res.Created)-1]
	if err := top.LoadIssue(ctx); err != nil {
		ctx.ServerError("LoadIssue", err)
		return
	}
	ctx.Flash.Success(ctx.Tr("repo.pulls.asc_op.reverted", len(res.Created)))
	ctx.Redirect(top.Issue.Link())
}
