// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package setting

import (
	"errors"

	git_model "forgejo.org/models/git"
	"forgejo.org/services/context"
)

// prepareAscRemovedRules lists the rules removed in the last 30 minutes, each with its Undo, on the rules page.
func prepareAscRemovedRules(ctx *context.Context) {
	removed, err := git_model.AscRemovedRules(ctx, ctx.Repo.Repository.ID)
	if err != nil {
		ctx.ServerError("AscRemovedRules", err)
		return
	}
	ctx.Data["AscRemovedRules"] = removed
}

// AscRestoreRulePost puts a removed branch rule back (board RepoSettingsRules: Undo for 30 minutes).
func AscRestoreRulePost(ctx *context.Context) {
	back := ctx.Repo.RepoLink + "/settings/branches"
	if ascRulesRepo(ctx) {
		ctx.Flash.Error(ctx.Tr("repo.settings.asc_rules.managed"))
		ctx.Redirect(ctx.Repo.RepoLink + "/settings/asc/policy")
		return
	}
	rule, err := git_model.AscRestoreRule(ctx, ctx.Repo.Repository, ctx.ParamsInt64("id"))
	var taken git_model.ErrAscRuleUndoTaken
	var overlap git_model.ErrAscRuleUndoOverlap
	switch {
	case err == nil:
		ctx.Flash.Success(ctx.Tr("repo.settings.asc_rules.restored", rule.RuleName))
	case errors.Is(err, git_model.ErrAscRuleUndoGone):
		ctx.Flash.Error(ctx.Tr("repo.settings.asc_rules.undo_gone"))
	case errors.As(err, &taken):
		ctx.Flash.Error(ctx.Tr("repo.settings.asc_rules.undo_taken", taken.Name))
	case errors.As(err, &overlap):
		ctx.Flash.Error(ctx.Tr("repo.settings.asc_rules.undo_overlap", overlap.Name, overlap.With))
	default:
		ctx.ServerError("AscRestoreRule", err)
		return
	}
	ctx.Redirect(back)
}
