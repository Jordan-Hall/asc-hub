// Copyright 2022 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package admin

import (
	"fmt"
	"net/http"

	"forgejo.org/models/auth"
	"forgejo.org/models/db"
	"forgejo.org/modules/base"
	"forgejo.org/modules/setting"
	user_setting "forgejo.org/routers/web/user/setting"
	"forgejo.org/services/context"
)

var (
	tplSettingsApplications          base.TplName = "admin/applications/list"
	tplSettingsOauth2ApplicationEdit base.TplName = "admin/applications/oauth2_edit"
)

func newOAuth2CommonHandlers() *user_setting.OAuth2CommonHandlers {
	return &user_setting.OAuth2CommonHandlers{
		OwnerID:            0,
		BasePathList:       fmt.Sprintf("%s/admin/applications", setting.AppSubURL),
		BasePathEditPrefix: fmt.Sprintf("%s/admin/applications/oauth2", setting.AppSubURL),
		TplAppEdit:         tplSettingsOauth2ApplicationEdit,
	}
}

// appOAuth2Handlers manages the application of the request, the site's or a person's: an admin manages every
// application on the site. An application that does not exist is left to the handler to answer.
func appOAuth2Handlers(ctx *context.Context) *user_setting.OAuth2CommonHandlers {
	oa := newOAuth2CommonHandlers()
	if app, err := auth.GetOAuth2ApplicationByID(ctx, ctx.ParamsInt64("id")); err == nil {
		oa.OwnerID = app.UID
	}
	return oa
}

// Applications render org applications page (for org, at the moment, there are only OAuth2 applications)
func Applications(ctx *context.Context) {
	ctx.Data["Title"] = ctx.Tr("settings.applications")
	ctx.Data["PageIsAdminApplications"] = true

	apps, err := db.Find[auth.OAuth2Application](ctx, auth.FindOAuth2ApplicationsOptions{
		IsGlobal: true,
	})
	if err != nil {
		ctx.ServerError("GetOAuth2ApplicationsByUserID", err)
		return
	}
	ctx.Data["Applications"] = apps
	ctx.Data["BuiltinApplications"] = auth.BuiltinApplications()
	ctx.HTML(http.StatusOK, tplSettingsApplications)
}

// ApplicationsPost response for adding an oauth2 application
func ApplicationsPost(ctx *context.Context) {
	ctx.Data["Title"] = ctx.Tr("settings.applications")
	ctx.Data["PageIsAdminApplications"] = true

	oa := newOAuth2CommonHandlers()
	oa.AddApp(ctx)
}

// EditApplication displays the given application
func EditApplication(ctx *context.Context) {
	ctx.Data["PageIsAdminApplications"] = true

	oa := appOAuth2Handlers(ctx)
	oa.EditShow(ctx)
}

// EditApplicationPost response for editing oauth2 application
func EditApplicationPost(ctx *context.Context) {
	ctx.Data["Title"] = ctx.Tr("settings.applications")
	ctx.Data["PageIsAdminApplications"] = true

	oa := appOAuth2Handlers(ctx)
	oa.EditSave(ctx)
}

// ApplicationsRegenerateSecret handles the post request for regenerating the secret
func ApplicationsRegenerateSecret(ctx *context.Context) {
	ctx.Data["Title"] = ctx.Tr("settings")
	ctx.Data["PageIsAdminApplications"] = true

	oa := appOAuth2Handlers(ctx)
	oa.RegenerateSecret(ctx)
}

// DeleteApplication deletes the given oauth2 application
func DeleteApplication(ctx *context.Context) {
	oa := appOAuth2Handlers(ctx)
	oa.DeleteApp(ctx)
}

// TODO: revokes the grant with the given id
