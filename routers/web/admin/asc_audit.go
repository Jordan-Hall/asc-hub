// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package admin

import (
	"net/http"

	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/base"
	"forgejo.org/modules/setting"
	"forgejo.org/routers/web/shared/trustweb"
	"forgejo.org/services/asctrust"
	"forgejo.org/services/context"
)

// The audit log of the whole server (docs/BUILDOUT.md section 3): /admin/asc/audit. Site administrators only.
const tplAscAudit base.TplName = "admin/asc_audit"

// ascAuditLevel builds the level of the instance, or writes the refusal and returns nil. It fails closed.
func ascAuditLevel(ctx *context.Context) *trustweb.AuditLevel {
	if !setting.Asc.Enabled {
		ctx.NotFound("asc audit", nil)
		return nil
	}
	if !ctx.IsSigned || ctx.Doer == nil || !ctx.Doer.IsAdmin {
		ctx.Error(http.StatusForbidden)
		return nil
	}
	ctx.Data["PageIsAdminAscAudit"] = true
	return &trustweb.AuditLevel{
		Scope: &ascv1.OwnerRef{Kind: ascv1.OwnerKind_OWNER_KIND_INSTANCE},
		Call:  asctrust.CallInstance,
		Base:  setting.AppSubURL + "/admin/asc/audit",
		Label: "this server",
		Tpl:   tplAscAudit,
		Slug:  "server",
	}
}

// AscAudit serves GET /admin/asc/audit.
func AscAudit(ctx *context.Context) {
	if lvl := ascAuditLevel(ctx); lvl != nil {
		trustweb.ServeAudit(ctx, lvl)
	}
}

// AscAuditExport serves GET /admin/asc/audit/export.
func AscAuditExport(ctx *context.Context) {
	if lvl := ascAuditLevel(ctx); lvl != nil {
		trustweb.ServeAuditExport(ctx, lvl)
	}
}

// AscAuditVerify serves POST /admin/asc/audit/verify.
func AscAuditVerify(ctx *context.Context) {
	if lvl := ascAuditLevel(ctx); lvl != nil {
		trustweb.ServeAuditVerify(ctx, lvl)
	}
}
