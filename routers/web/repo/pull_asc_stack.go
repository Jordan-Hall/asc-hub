// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	issues_model "forgejo.org/models/issues"
	"forgejo.org/services/context"
	pull_service "forgejo.org/services/pull"
)

// The stack panel and the mid-stack merge block of the pull request page (SPEC 20.5, PLAN unit C36). The
// template is templates/repo/pulls/asc_stack.tmpl.

// prepareAscStackPanel puts the stack panel of a stacked pull request into ctx.Data. It does nothing for a
// pull request that is not stacked, and reports false after it has answered with an error.
func prepareAscStackPanel(ctx *context.Context, pull *issues_model.PullRequest) bool {
	panel, err := pull_service.BuildAscStackPanel(ctx, pull)
	if err != nil {
		ctx.ServerError("BuildAscStackPanel", err)
		return false
	}
	if panel != nil {
		ctx.Data["AscStackPanel"] = panel
		prepareAscStackActions(ctx, pull)
	}
	return true
}

// ascStackMergeBlocked answers a web merge of a pull request whose parent has not merged with the JSON
// error of the merge form, and reports true when it did. A lookup error also stops the merge.
func ascStackMergeBlocked(ctx *context.Context, pr *issues_model.PullRequest) bool {
	parent, err := pull_service.AscStackParentBlocking(ctx, pr)
	if err != nil {
		ctx.ServerError("AscStackParentBlocking", err)
		return true
	}
	if parent == nil {
		return false
	}
	ctx.JSONError(ctx.Tr("repo.pulls.asc_stack_parent_unmerged"))
	return true
}
