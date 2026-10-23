// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package setting

import (
	"fmt"
	"net/url"

	git_model "forgejo.org/models/git"
	"forgejo.org/modules/setting"
	"forgejo.org/services/context"
	"forgejo.org/services/forms"
)

// prepareAscRelaxation offers the asc relaxed approval settings on the rule page while [asc] is enabled.
func prepareAscRelaxation(ctx *context.Context) {
	if setting.Asc.Enabled {
		ctx.Data["AscRelaxableClasses"] = git_model.AscRelaxableClasses()
	}
}

// applyAscRelaxation copies the relaxed approval settings from the form to the rule. It returns false,
// after redirecting with an error, when the form is not acceptable. While [asc] is disabled the form
// does not carry the settings, so the stored values are kept.
func applyAscRelaxation(ctx *context.Context, f *forms.ProtectBranchForm, protectBranch *git_model.ProtectedBranch) bool {
	if !setting.Asc.Enabled {
		return true
	}
	classes, err := git_model.NormalizeAscRelaxedClasses(f.AscRelaxedClasses)
	if err == nil {
		err = git_model.ValidateAscRelaxedApprovals(f.AscRelaxedApprovals)
	}
	if err != nil {
		ctx.Flash.Error(ctx.Tr("repo.settings.protect_asc_relaxed_invalid"))
		ctx.Redirect(fmt.Sprintf("%s/settings/branches/edit?rule_name=%s", ctx.Repo.RepoLink, url.QueryEscape(f.RuleName)))
		return false
	}
	protectBranch.AscRelaxedClasses = classes
	protectBranch.AscRelaxedApprovals = f.AscRelaxedApprovals
	return true
}
