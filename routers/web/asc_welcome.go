// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package web

import (
	"net/http"

	"forgejo.org/models/asymkey"
	"forgejo.org/models/auth"
	"forgejo.org/models/db"
	"forgejo.org/modules/setting"
	"forgejo.org/services/context"
)

const tplAscWelcome = "user/asc_welcome"

// Welcome renders /user/welcome, the first run page of a new account: bring a repository, connect a
// computer. Everything it says about the account is read from the account itself (repositories, keys,
// tokens, a confirmed address), so each line is true for the person looking at it. The page is read only
// and needs a signed in user.
func Welcome(ctx *context.Context) {
	doer := ctx.Doer
	keys, err := db.Count[asymkey.PublicKey](ctx, asymkey.FindPublicKeyOptions{OwnerID: doer.ID})
	if err != nil {
		ctx.ServerError("CountPublicKeys", err)
		return
	}
	tokens, err := db.Count[auth.AccessToken](ctx, auth.ListAccessTokensOptions{UserID: doer.ID})
	if err != nil {
		ctx.ServerError("CountAccessTokens", err)
		return
	}
	prepareAscDownload(ctx)
	ctx.Data["Title"] = ctx.Locale.TrString("asc_welcome.title")
	ctx.Data["PageIsAscWelcome"] = true
	ctx.Data["WelcomeRepos"] = doer.NumRepos
	ctx.Data["WelcomeKeys"] = keys
	ctx.Data["WelcomeTokens"] = tokens
	ctx.Data["WelcomeActive"] = doer.IsActive
	ctx.Data["WelcomeSSH"] = !setting.SSH.Disabled
	ctx.Data["WelcomeMigrations"] = !setting.Repository.DisableMigrations
	ctx.HTML(http.StatusOK, tplAscWelcome)
}
