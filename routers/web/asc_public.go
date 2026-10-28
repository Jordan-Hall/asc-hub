// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package web

import (
	"net/http"

	"forgejo.org/models/asymkey"
	"forgejo.org/models/auth"
	"forgejo.org/models/db"
	"forgejo.org/modules/ascdownload"
	"forgejo.org/modules/setting"
	"forgejo.org/services/context"
)

// The public pages of the hub: about, documentation start page and the get started checklist.
const (
	tplAscAbout      = "asc/about"
	tplAscDocs       = "asc/docs"
	tplAscGetStarted = "asc/get_started"
)

// AscAbout serves GET /-/about.
func AscAbout(ctx *context.Context) {
	prepareAscDownload(ctx)
	ctx.Data["Title"] = ctx.Locale.TrString("asc_site.about_title")
	ctx.Data["PageIsAscAbout"] = true
	ctx.HTML(http.StatusOK, tplAscAbout)
}

// AscDocs serves GET /-/docs: the start page of the documentation, which lives in the docs folder of the source.
func AscDocs(ctx *context.Context) {
	ctx.Data["Title"] = ctx.Locale.TrString("asc_site.docs_title")
	ctx.Data["PageIsAscDocs"] = true
	ctx.HTML(http.StatusOK, tplAscDocs)
}

// AscGetStarted serves GET /-/get-started: the checklist of a person whose hub is new. It marks a step done only
// when the hub can see it is: a repository exists, an access token exists.
func AscGetStarted(ctx *context.Context) {
	doer := ctx.Doer
	tokens, err := db.Count[auth.AccessToken](ctx, auth.ListAccessTokensOptions{UserID: doer.ID})
	if err != nil {
		ctx.ServerError("CountAccessTokens", err)
		return
	}
	keys, err := db.Count[asymkey.PublicKey](ctx, asymkey.FindPublicKeyOptions{OwnerID: doer.ID})
	if err != nil {
		ctx.ServerError("CountPublicKeys", err)
		return
	}
	prepareAscDownload(ctx)
	done := 0
	connected := tokens > 0
	hasRepo := doer.NumRepos > 0
	if connected {
		done++
	}
	if hasRepo {
		done++
	}
	ctx.Data["Title"] = ctx.Locale.TrString("asc_site.started_title")
	ctx.Data["PageIsAscGetStarted"] = true
	ctx.Data["GSConnected"] = connected
	ctx.Data["GSHasRepo"] = hasRepo
	ctx.Data["GSKeys"] = keys
	ctx.Data["GSDone"] = done
	ctx.Data["GSTotal"] = 4
	ctx.Data["GSPercent"] = done * 100 / 4
	ctx.Data["GSMigrations"] = !setting.Repository.DisableMigrations
	ctx.Data["GSConnectCommand"] = ascdownload.ConnectCommand(setting.AppURL)
	ctx.HTML(http.StatusOK, tplAscGetStarted)
}
