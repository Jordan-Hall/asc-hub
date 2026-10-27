// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	"forgejo.org/modules/base"
	"forgejo.org/modules/setting"
	"forgejo.org/routers/web/shared/trustweb"
	"forgejo.org/services/asctrust"
	"forgejo.org/services/context"

	ascv1 "forgejo.org/modules/ascproto/ascv1"
)

// The audit log of a repository (docs/BUILDOUT.md section 3): /{owner}/{repo}/settings/asc/audit, with the filters,
// the export and the chain check. The page is for administrators of the repository: Forgejo checks that here and
// asc checks it again, because the calls are made with a short-lived credential of the person.
const tplAscAudit base.TplName = "repo/asc_audit"

// ascAuditLevel builds the level of the repository, or writes the refusal and returns nil.
func ascAuditLevel(ctx *context.Context) *trustweb.AuditLevel {
	repo, name, ok := trustweb.AdminRepo(ctx)
	if !ok {
		return nil
	}
	ctx.Data["PageIsSettingsAscAudit"] = true
	return &trustweb.AuditLevel{
		Scope: &ascv1.OwnerRef{Kind: ascv1.OwnerKind_OWNER_KIND_REPO, Name: name},
		Call:  asctrust.CallRepoRead,
		Repo:  repo,
		Base:  setting.AppSubURL + "/" + repo.OwnerName + "/" + repo.Name + "/settings/asc/audit",
		Label: repo.FullName(),
		Tpl:   tplAscAudit,
		Slug:  repo.OwnerName + "-" + repo.Name,
	}
}

// AscAudit serves GET /{owner}/{repo}/settings/asc/audit.
func AscAudit(ctx *context.Context) {
	if lvl := ascAuditLevel(ctx); lvl != nil {
		trustweb.ServeAudit(ctx, lvl)
	}
}

// AscAuditExport serves GET /{owner}/{repo}/settings/asc/audit/export.
func AscAuditExport(ctx *context.Context) {
	if lvl := ascAuditLevel(ctx); lvl != nil {
		trustweb.ServeAuditExport(ctx, lvl)
	}
}

// AscAuditVerify serves POST /{owner}/{repo}/settings/asc/audit/verify.
func AscAuditVerify(ctx *context.Context) {
	if lvl := ascAuditLevel(ctx); lvl != nil {
		trustweb.ServeAuditVerify(ctx, lvl)
	}
}
