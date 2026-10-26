// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package setting

import (
	"net/http"

	"forgejo.org/modules/base"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/web"
	"forgejo.org/routers/web/shared/byoweb"
	svc "forgejo.org/services/ascbyo"
	"forgejo.org/services/context"
)

// The "Storage" page of the account settings (/user/settings/asc/storage): the signed-in user's own
// S3-compatible targets. The page is for the account of the signed-in user and has no way to name another
// one: the scope is built from ctx.Doer, never from the request. Everything else is in byoweb.
const (
	tplSettingsAscStorage base.TplName = "user/settings/asc_storage"
	tplSettingsAscAI      base.TplName = "user/settings/asc_ai"
)

// ascByoLevel is the Resolver of the account level. It fails closed: no asc, no signed-in user or an
// organisation account (which has its own settings) is a 404.
func ascByoLevel(ctx *context.Context) *byoweb.Level {
	if !setting.Asc.Enabled || !ctx.IsSigned || ctx.Doer == nil || ctx.Doer.IsOrganization() {
		ctx.NotFound("asc byo", nil)
		return nil
	}
	if ctx.Doer.ProhibitLogin {
		ctx.Error(http.StatusForbidden)
		return nil
	}
	lvl, err := byoweb.NewLevel(svc.KindUser, ctx.Doer.LowerName, ctx.Doer.Name, setting.AppSubURL+"/user/settings/asc")
	if err != nil {
		ctx.NotFound("asc byo scope", err)
		return nil
	}
	lvl.StorageTpl, lvl.AITpl = tplSettingsAscStorage, tplSettingsAscAI
	return lvl
}

// AscRoutes registers the Storage and AI keys pages below /user/settings.
func AscRoutes(m *web.Route) { byoweb.Routes(m, ascByoLevel) }
