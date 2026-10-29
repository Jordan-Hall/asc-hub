// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package setting

import (
	"net/http"
	"strings"

	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/base"
	"forgejo.org/modules/setting"
	"forgejo.org/routers/web/shared/usageweb"
	"forgejo.org/services/asctrust"
	"forgejo.org/services/context"
)

// AscUsage serves GET /org/{org}/settings/asc/usage: the usage of the organisation for a month. Only an owner of the
// organisation, or a site administrator, may open it, and the organisation is the one of the route.
func AscUsage(ctx *context.Context) {
	if !setting.Asc.Enabled || ctx.Org == nil || ctx.Org.Organization == nil {
		ctx.NotFound("asc usage", nil)
		return
	}
	if !ctx.IsSigned || ctx.Doer == nil || !(ctx.Org.IsOwner || ctx.Doer.IsAdmin) {
		ctx.Error(http.StatusForbidden)
		return
	}
	org := ctx.Org.Organization
	ctx.Data["PageIsSettingsAscUsage"] = true
	usageweb.Serve(ctx, &usageweb.Level{
		Owner: &ascv1.OwnerRef{Kind: ascv1.OwnerKind_OWNER_KIND_ORG, Name: strings.ToLower(org.Name)},
		Call:  asctrust.CallOrg,
		Org:   org.AsUser(),
		Base:  setting.AppSubURL + "/org/" + org.Name + "/settings/asc/usage",
		Label: org.Name,
		Tpl:   base.TplName("org/settings/asc_usage"),
	})
}
