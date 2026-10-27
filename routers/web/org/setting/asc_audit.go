// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package setting

import (
	"net/http"
	"strings"

	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/base"
	"forgejo.org/modules/setting"
	"forgejo.org/routers/web/shared/trustweb"
	"forgejo.org/services/asctrust"
	"forgejo.org/services/context"
)

// The audit log of an organisation (docs/BUILDOUT.md section 3): /org/{org}/settings/asc/audit. Only an owner of
// the organisation, or a site administrator, may open it, and the organisation is the one of the route.
const tplSettingsAscAudit base.TplName = "org/settings/asc_audit"

// ascAuditLevel builds the level of the organisation, or writes the refusal and returns nil. It fails closed.
func ascAuditLevel(ctx *context.Context) *trustweb.AuditLevel {
	if !setting.Asc.Enabled || ctx.Org == nil || ctx.Org.Organization == nil {
		ctx.NotFound("asc audit", nil)
		return nil
	}
	if !ctx.IsSigned || ctx.Doer == nil || !(ctx.Org.IsOwner || ctx.Doer.IsAdmin) {
		ctx.Error(http.StatusForbidden)
		return nil
	}
	org := ctx.Org.Organization
	ctx.Data["PageIsSettingsAscAudit"] = true
	return &trustweb.AuditLevel{
		Scope: &ascv1.OwnerRef{Kind: ascv1.OwnerKind_OWNER_KIND_ORG, Name: strings.ToLower(org.Name)},
		Call:  asctrust.CallOrg,
		Base:  setting.AppSubURL + "/org/" + org.Name + "/settings/asc/audit",
		Label: org.Name,
		Tpl:   tplSettingsAscAudit,
		Slug:  org.Name,
	}
}

// AscAudit serves GET /org/{org}/settings/asc/audit.
func AscAudit(ctx *context.Context) {
	if lvl := ascAuditLevel(ctx); lvl != nil {
		trustweb.ServeAudit(ctx, lvl)
	}
}

// AscAuditExport serves GET /org/{org}/settings/asc/audit/export.
func AscAuditExport(ctx *context.Context) {
	if lvl := ascAuditLevel(ctx); lvl != nil {
		trustweb.ServeAuditExport(ctx, lvl)
	}
}

// AscAuditVerify serves POST /org/{org}/settings/asc/audit/verify.
func AscAuditVerify(ctx *context.Context) {
	if lvl := ascAuditLevel(ctx); lvl != nil {
		trustweb.ServeAuditVerify(ctx, lvl)
	}
}
