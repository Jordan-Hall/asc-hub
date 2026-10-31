// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package admin

import (
	"net/http"

	"forgejo.org/modules/base"
	"forgejo.org/modules/log"
	"forgejo.org/modules/setting"
	"forgejo.org/services/context"
	packages_cleanup_service "forgejo.org/services/packages/cleanup"
)

const tplPackagesCleanupRules base.TplName = "admin/packages/cleanup_rules"

// CleanupRulesPreview serves GET /admin/packages/cleanup/rules: every enabled cleanup rule with the number of versions it
// would delete if it ran now, and the button that runs them. Nothing is deleted by looking.
func CleanupRulesPreview(ctx *context.Context) {
	ctx.Data["Title"] = ctx.Tr("asc_admin.packages")
	ctx.Data["PageIsAdminPackages"] = true
	previews, err := packages_cleanup_service.PreviewRules(ctx)
	if err != nil {
		ctx.ServerError("PreviewRules", err)
		return
	}
	total := 0
	for _, p := range previews {
		total += p.Versions
	}
	ctx.Data["RulePreviews"] = previews
	ctx.Data["RulePreviewTotal"] = total
	ctx.HTML(http.StatusOK, tplPackagesCleanupRules)
}

// CleanupRulesRun serves POST /admin/packages/cleanup/rules: it runs the enabled cleanup rules once, as the daily task does.
func CleanupRulesRun(ctx *context.Context) {
	if err := packages_cleanup_service.ExecuteCleanupRules(ctx); err != nil {
		log.Error("admin: running the package cleanup rules: %v", err)
		ctx.Flash.Error(ctx.Tr("asc_admin.packages_rules_failed"))
	} else {
		ctx.Flash.Success(ctx.Tr("asc_admin.packages_rules_done"))
	}
	ctx.Redirect(setting.AppSubURL + "/admin/packages/cleanup/rules")
}
