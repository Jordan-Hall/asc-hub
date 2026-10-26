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

// The "Storage" and "AI keys" pages of an organisation (/org/{org}/settings/asc/...): targets and keys
// that belong to the organisation. Only an owner of the organisation (or a site administrator, whom Forgejo
// treats as one) may open them, and the organisation is the one of the route.
const (
	tplSettingsAscStorage base.TplName = "org/settings/asc_storage"
	tplSettingsAscAI      base.TplName = "org/settings/asc_ai"
)

// ascByoLevel is the Resolver of the organisation level. It fails closed.
func ascByoLevel(ctx *context.Context) *byoweb.Level {
	if !setting.Asc.Enabled || !ctx.IsSigned || ctx.Doer == nil || ctx.Org == nil || ctx.Org.Organization == nil {
		ctx.NotFound("asc byo", nil)
		return nil
	}
	if !ctx.Org.IsOwner {
		ctx.Error(http.StatusForbidden)
		return nil
	}
	org := ctx.Org.Organization
	lvl, err := byoweb.NewLevel(svc.KindOrg, org.LowerName, org.Name, setting.AppSubURL+"/org/"+org.Name+"/settings/asc")
	if err != nil {
		ctx.NotFound("asc byo scope", err)
		return nil
	}
	lvl.StorageTpl, lvl.AITpl = tplSettingsAscStorage, tplSettingsAscAI
	return lvl
}

// AscRoutes registers the Storage and AI keys pages below /org/{org}/settings.
func AscRoutes(m *web.Route) { byoweb.Routes(m, ascByoLevel) }
