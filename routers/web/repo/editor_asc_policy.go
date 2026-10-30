// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	"errors"

	"forgejo.org/modules/base"
	"forgejo.org/services/context"
	files_service "forgejo.org/services/repository/files"
)

// ascPolicyEditOutcome answers a save or delete of the file editor that the policy hook took over
// (services/asctrust): the policy file of a repository stored in asc is proposed as a change under review and never
// committed to the default branch, and only a person asc lets write the policy file may propose it. It returns false
// for any other error.
func ascPolicyEditOutcome(ctx *context.Context, err error, tpl base.TplName, form any) bool {
	var proposed *files_service.ErrAscPolicyProposed
	if errors.As(err, &proposed) {
		ctx.Flash.Success(ctx.Tr("repo.editor.asc_policy_proposed", proposed.Change))
		if proposed.Link != "" {
			ctx.Redirect(proposed.Link)
		} else {
			ctx.Redirect(ctx.Repo.RepoLink + "/settings/asc/policy")
		}
		return true
	}
	var refused *files_service.ErrAscPolicyRefused
	if errors.As(err, &refused) {
		ctx.Data["Err_TreePath"] = true
		ctx.RenderWithErr(refused.Reason, tpl, form)
		return true
	}
	return false
}
