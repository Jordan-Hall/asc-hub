// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	"net/http"

	issues_model "forgejo.org/models/issues"
	api "forgejo.org/modules/structs"
	"forgejo.org/services/context"
	pull_service "forgejo.org/services/pull"
)

// ascStackParentUnmerged is the message of the 409 that POST /pulls/{n}/merge returns for a pull request
// whose parent pull request has not merged (SPEC 20.5, PLAN unit C36). The URL points at the parent.
const ascStackParentUnmerged = "stack-parent-unmerged"

// ascMergeGuard answers 409 stack-parent-unmerged when pr is blocked by an unmerged parent pull request,
// and reports true when it answered. A lookup error answers 500: it never lets the merge go on.
func ascMergeGuard(ctx *context.APIContext, pr *issues_model.PullRequest) bool {
	parent, err := pull_service.AscStackParentBlocking(ctx, pr)
	if err != nil {
		ctx.Error(http.StatusInternalServerError, "AscStackParentBlocking", err)
		return true
	}
	if parent == nil {
		return false
	}
	url := ""
	if err := parent.LoadIssue(ctx); err == nil {
		if err := parent.LoadBaseRepo(ctx); err == nil {
			parent.Issue.Repo = parent.BaseRepo
			url = parent.Issue.HTMLURL()
		}
	}
	ctx.JSON(http.StatusConflict, api.APIError{Message: ascStackParentUnmerged, URL: url})
	return true
}
