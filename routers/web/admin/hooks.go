// Copyright 2018 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package admin

import (
	"net/http"

	"forgejo.org/models/auth"
	"forgejo.org/models/db"
	user_model "forgejo.org/models/user"
	"forgejo.org/models/webhook"
	"forgejo.org/modules/base"
	"forgejo.org/modules/setting"
	"forgejo.org/services/context"
	webhook_service "forgejo.org/services/webhook"
)

const (
	// tplAdminHooks template path to render hook settings
	tplAdminHooks base.TplName = "admin/hooks"
)

// DefaultOrSystemWebhooks renders both admin default and system webhook list pages
func DefaultOrSystemWebhooks(ctx *context.Context) {
	var err error

	ctx.Data["Title"] = ctx.Tr("admin.hooks")
	ctx.Data["PageIsAdminSystemHooks"] = true
	ctx.Data["PageIsAdminDefaultHooks"] = true

	def := make(map[string]any, len(ctx.Data))
	sys := make(map[string]any, len(ctx.Data))
	for k, v := range ctx.Data {
		def[k] = v
		sys[k] = v
	}

	sys["Title"] = ctx.Tr("admin.systemhooks")
	sys["Description"] = ctx.Tr("admin.systemhooks.desc", "https://forgejo.org/docs/latest/user/webhooks/")
	sysHooks, _, err := webhook.GetSystemWebhooks(ctx, db.ListOptions{ListAll: true}, false)
	sys["BaseLink"] = setting.AppSubURL + "/admin/hooks"
	sys["BaseLinkNew"] = setting.AppSubURL + "/admin/system-hooks"
	sys["WebhookList"] = webhook_service.List()
	if err != nil {
		ctx.ServerError("GetWebhooksAdmin", err)
		return
	}

	def["Title"] = ctx.Tr("admin.defaulthooks")
	def["Description"] = ctx.Tr("admin.defaulthooks.desc", "https://forgejo.org/docs/latest/user/webhooks/")
	defHooks, _, err := webhook.GetDefaultWebhooks(ctx)
	def["BaseLink"] = setting.AppSubURL + "/admin/hooks"
	def["BaseLinkNew"] = setting.AppSubURL + "/admin/default-hooks"
	def["WebhookList"] = webhook_service.List()
	if err != nil {
		ctx.ServerError("GetWebhooksAdmin", err)
		return
	}

	// One list of both, each row saying which it is
	rows, err := webhook_service.ListRows(ctx, append(sysHooks, defHooks...))
	if err != nil {
		ctx.ServerError("ListRows", err)
		return
	}
	for i, r := range rows {
		if i < len(sysHooks) {
			r.Scope = ctx.Locale.TrString("asc_admin.hooks_scope_system")
		} else {
			r.Scope = ctx.Locale.TrString("asc_admin.hooks_scope_default")
		}
	}
	ctx.Data["Webhooks"] = rows

	ctx.Data["EnableOAuth2"] = setting.OAuth2.Enabled
	if setting.OAuth2.Enabled {
		apps, err := oauthAppRows(ctx)
		if err != nil {
			ctx.ServerError("oauthAppRows", err)
			return
		}
		ctx.Data["Apps"] = apps
	}

	ctx.Data["DefaultWebhooks"] = def
	ctx.Data["SystemWebhooks"] = sys

	ctx.HTML(http.StatusOK, tplAdminHooks)
}

// DeleteDefaultOrSystemWebhook handler to delete an admin-defined system or default webhook
func DeleteDefaultOrSystemWebhook(ctx *context.Context) {
	if err := webhook.DeleteDefaultSystemWebhook(ctx, ctx.FormInt64("id")); err != nil {
		ctx.Flash.Error("DeleteDefaultWebhook: " + err.Error())
	} else {
		ctx.Flash.Success(ctx.Tr("repo.settings.webhook_deletion_success"))
	}

	ctx.JSONRedirect(setting.AppSubURL + "/admin/hooks")
}

// appRow is one OAuth application in the site-wide list: who owns it and how many people granted it access.
type appRow struct {
	*auth.OAuth2Application
	// Owner is nil for an application of the site.
	Owner  *user_model.User
	Grants int64
}

// ShortClientID is the client ID with its middle left out, enough to tell applications apart in a list.
func (r *appRow) ShortClientID() string {
	if len(r.ClientID) <= 12 {
		return r.ClientID
	}
	return r.ClientID[:4] + "…" + r.ClientID[len(r.ClientID)-4:]
}

// oauthAppRows lists the OAuth applications registered on the site, by the site and by people, with each one's owner
// and grants. The built-in applications that ship with the hub are left to the applications page.
func oauthAppRows(ctx *context.Context) ([]*appRow, error) {
	all, err := db.Find[auth.OAuth2Application](ctx, auth.FindOAuth2ApplicationsOptions{ListOptions: db.ListOptions{ListAll: true}})
	if err != nil {
		return nil, err
	}
	builtin := auth.BuiltinApplications()
	apps := make([]*auth.OAuth2Application, 0, len(all))
	for _, a := range all {
		if _, ok := builtin[a.ClientID]; !ok {
			apps = append(apps, a)
		}
	}
	ids := make([]int64, len(apps))
	ownerIDs := make([]int64, 0, len(apps))
	for i, a := range apps {
		ids[i] = a.ID
		if a.UID != 0 {
			ownerIDs = append(ownerIDs, a.UID)
		}
	}
	grants, err := auth.CountOAuth2Grants(ctx, ids)
	if err != nil {
		return nil, err
	}
	owners, err := user_model.GetPossibleUserByIDs(ctx, ownerIDs)
	if err != nil {
		return nil, err
	}
	byID := make(map[int64]*user_model.User, len(owners))
	for _, u := range owners {
		byID[u.ID] = u
	}
	rows := make([]*appRow, len(apps))
	for i, a := range apps {
		rows[i] = &appRow{OAuth2Application: a, Owner: byID[a.UID], Grants: grants[a.ID]}
	}
	return rows, nil
}
