// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package setting

import (
	"net/http"

	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/base"
	"forgejo.org/modules/setting"
	"forgejo.org/routers/web/shared/usageweb"
	"forgejo.org/services/asctrust"
	"forgejo.org/services/context"
)

// AscUsage serves GET /user/settings/asc/usage: the usage of the signed-in person for a month (docs/BUILDOUT.md
// section 27). The owner is the person, never a name from the request.
func AscUsage(ctx *context.Context) {
	if !setting.Asc.Enabled {
		ctx.NotFound("asc usage", nil)
		return
	}
	if !ctx.IsSigned || ctx.Doer == nil || ctx.Doer.IsOrganization() {
		ctx.Error(http.StatusForbidden)
		return
	}
	ctx.Data["PageIsSettingsAscUsage"] = true
	usageweb.Serve(ctx, &usageweb.Level{
		Owner: &ascv1.OwnerRef{Kind: ascv1.OwnerKind_OWNER_KIND_USER, Name: ctx.Doer.LowerName},
		Call:  asctrust.CallAccount,
		Base:  setting.AppSubURL + "/user/settings/asc/usage",
		Label: ctx.Doer.Name,
		Tpl:   base.TplName("user/settings/asc_usage"),
	})
}
