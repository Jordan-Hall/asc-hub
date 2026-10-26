// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package setting

import (
	"net/http"
	"strings"

	"forgejo.org/models/organization"
	"forgejo.org/modules/base"
	"forgejo.org/modules/log"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/web"
	"forgejo.org/routers/web/shared/byoweb"
	svc "forgejo.org/services/ascbyo"
	"forgejo.org/services/ascsync"
	"forgejo.org/services/context"
)

// The "Storage" and "AI keys" pages of a repository (/{owner}/{repo}/settings/asc/...). They apply to
// repositories stored in asc and to administrators of the repository only. A repository may keep targets and
// keys of its own, and may use the targets of its owner when the signed-in user administers the owner too
// (assigning a target needs both rights).
const (
	tplSettingsAscStorage base.TplName = "repo/settings/asc_storage"
	tplSettingsAscAI      base.TplName = "repo/settings/asc_ai"
)

// ascByoLevel is the Resolver of the repository level. It fails closed.
func ascByoLevel(ctx *context.Context) *byoweb.Level {
	repo := ctx.Repo.Repository
	if !setting.Asc.Enabled || !ctx.IsSigned || ctx.Doer == nil || repo == nil || ctx.Repo.Owner == nil {
		ctx.NotFound("asc byo", nil)
		return nil
	}
	if ok, err := ascsync.BackedBy(repo); err != nil || !ok {
		ctx.NotFound("asc byo: not an asc repository", err)
		return nil
	}
	if !ctx.Repo.IsAdmin() {
		ctx.Error(http.StatusForbidden)
		return nil
	}
	link := setting.AppSubURL + "/" + repo.OwnerName + "/" + repo.Name + "/settings/asc"
	lvl, err := byoweb.NewLevel(svc.KindRepo, strings.ToLower(repo.OwnerName+"/"+repo.Name), repo.FullName(), link)
	if err != nil {
		ctx.NotFound("asc byo scope", err)
		return nil
	}
	lvl.StorageTpl, lvl.AITpl = tplSettingsAscStorage, tplSettingsAscAI
	lvl.Allowed = []svc.Scope{lvl.Scope}

	// The owner's targets may be used when the user administers the owner as well.
	owner := ctx.Repo.Owner
	adminOfOwner := ctx.Doer.IsAdmin
	ownerKind := svc.KindUser
	if owner.IsOrganization() {
		ownerKind = svc.KindOrg
		if !adminOfOwner {
			is, err := organization.IsOrganizationOwner(ctx, owner.ID, ctx.Doer.ID)
			if err != nil {
				log.Error("asc byo: owner check of %s: %v", repo.FullName(), err)
				ctx.ServerError("IsOrganizationOwner", err)
				return nil
			}
			adminOfOwner = is
		}
	} else {
		adminOfOwner = adminOfOwner || ctx.Doer.ID == owner.ID
	}
	if adminOfOwner {
		if sc, err := svc.NewScope(ownerKind, owner.LowerName); err == nil {
			lvl.Allowed = append(lvl.Allowed, sc)
			switch {
			case ownerKind == svc.KindOrg:
				lvl.OwnerStorageURL = setting.AppSubURL + "/org/" + owner.Name + "/settings/asc/storage"
				lvl.OwnerAIURL = setting.AppSubURL + "/org/" + owner.Name + "/settings/asc/ai"
			case owner.ID == ctx.Doer.ID:
				lvl.OwnerStorageURL = setting.AppSubURL + "/user/settings/asc/storage"
				lvl.OwnerAIURL = setting.AppSubURL + "/user/settings/asc/ai"
			}
		}
	}
	return lvl
}

// AscRoutes registers the Storage and AI keys pages below /{owner}/{repo}/settings.
func AscRoutes(m *web.Route) { byoweb.Routes(m, ascByoLevel) }
