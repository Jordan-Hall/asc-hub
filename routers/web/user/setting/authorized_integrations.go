// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package setting

import (
	"net/http"

	auth_model "forgejo.org/models/auth"
	"forgejo.org/models/db"
	"forgejo.org/modules/base"
	"forgejo.org/modules/optional"
	"forgejo.org/services/context"
)

const (
	tplSettingsAuthorizedIntegrations base.TplName = "user/settings/authorized_integrations"
)

func ListAuthorizedIntegrations(ctx *context.Context) {
	ctx.Data["Title"] = ctx.Tr("settings.authorized_integrations")
	ctx.Data["PageIsSettingsAuthorizedIntegrations"] = true

	ais, err := db.Find[auth_model.AuthorizedIntegration](ctx,
		auth_model.ListAuthorizedIntegrationOptions{UserID: optional.Some(ctx.Doer.ID)})
	if err != nil {
		ctx.ServerError("ListAuthorizedIntegrations", err)
		return
	}
	ctx.Data["AuthorizedIntegrations"] = ais

	ctx.HTML(http.StatusOK, tplSettingsAuthorizedIntegrations)
}
