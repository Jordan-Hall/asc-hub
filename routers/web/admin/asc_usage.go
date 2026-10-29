// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package admin

import (
	"net/http"

	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/base"
	"forgejo.org/modules/setting"
	"forgejo.org/routers/web/shared/usageweb"
	"forgejo.org/services/asctrust"
	"forgejo.org/services/context"
)

// The usage of the whole server, of any account or organisation (query kind and name), and the limits:
// /admin/asc/usage. Site administrators only.
func ascUsageLevel(ctx *context.Context) *usageweb.Level {
	if !setting.Asc.Enabled {
		ctx.NotFound("asc usage", nil)
		return nil
	}
	if !ctx.IsSigned || ctx.Doer == nil || !ctx.Doer.IsAdmin {
		ctx.Error(http.StatusForbidden)
		return nil
	}
	ctx.Data["PageIsAdminAscUsage"] = true
	return &usageweb.Level{
		Owner: &ascv1.OwnerRef{Kind: ascv1.OwnerKind_OWNER_KIND_INSTANCE},
		Call:  asctrust.CallInstance,
		Base:  setting.AppSubURL + "/admin/asc/usage",
		Label: "this server",
		Tpl:   base.TplName("admin/asc_usage"),
		Admin: true,
	}
}

// AscUsage serves GET /admin/asc/usage.
func AscUsage(ctx *context.Context) {
	if lvl := ascUsageLevel(ctx); lvl != nil {
		usageweb.Serve(ctx, lvl)
	}
}

// AscUsageQuota serves POST /admin/asc/usage/quota.
func AscUsageQuota(ctx *context.Context) {
	if lvl := ascUsageLevel(ctx); lvl != nil {
		usageweb.ServeSetQuota(ctx, lvl)
	}
}
