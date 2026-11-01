// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package security

import (
	"net/http"
	"net/url"
	"time"

	auth_model "forgejo.org/models/auth"
	"forgejo.org/modules/base"
	"forgejo.org/modules/httplib"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/timeutil"
	"forgejo.org/services/context"
)

const tplSettingsReauth base.TplName = "user/settings/security/reauth"


// ReauthFor is how long a proof lasts before a change to the sign-in of the account asks for the password again.
var ReauthFor = 10 * time.Minute

// recentlyAuthenticated says whether the doer proved it is them within ReauthFor. A person without a password (signed
// in through a provider only) has nothing to type here; the second factor and the provider guard them.
func recentlyAuthenticated(ctx *context.Context) bool {
	if !ctx.Doer.IsPasswordSet() {
		return true
	}
	at, _ := ctx.Session.Get(auth_model.ReauthSessionKey).(int64)
	return at != 0 && time.Since(time.Unix(at, 0)) < ReauthFor
}

func reauthURL(back string) string {
	return setting.AppSubURL + "/user/settings/security/reauth?redirect_to=" + url.QueryEscape(back)
}

// RequireReauth guards a change to how the account signs in (removing or replacing the second factor, a passkey, the
// recovery codes, signing other browsers out): when the last proof is too old it sends the person to the confirm page
// first. A request made by a script on the page gets the redirect as JSON.
func RequireReauth(json bool) func(ctx *context.Context) {
	return func(ctx *context.Context) {
		if recentlyAuthenticated(ctx) {
			return
		}
		back := setting.AppSubURL + "/user/settings/security"
		if ctx.Req.Method == http.MethodGet {
			back = ctx.Req.URL.RequestURI()
		}
		if json {
			ctx.JSONRedirect(reauthURL(back))
			return
		}
		ctx.Redirect(reauthURL(back))
	}
}

// Reauth serves `GET /user/settings/security/reauth`: the password, once more, before a change to the sign-in.
func Reauth(ctx *context.Context) {
	ctx.Data["Title"] = ctx.Tr("settings.reauth_title")
	ctx.Data["PageIsSettingsSecurity"] = true
	ctx.Data["RedirectTo"] = safeBack(ctx.FormString("redirect_to"))
	ctx.HTML(http.StatusOK, tplSettingsReauth)
}

// ReauthPost serves `POST /user/settings/security/reauth` with the fields password and redirect_to.
func ReauthPost(ctx *context.Context) {
	back := safeBack(ctx.FormString("redirect_to"))
	if !ctx.Doer.IsPasswordSet() || !ctx.Doer.ValidatePassword(ctx, ctx.FormString("password")) {
		ctx.Data["Title"] = ctx.Tr("settings.reauth_title")
		ctx.Data["PageIsSettingsSecurity"] = true
		ctx.Data["RedirectTo"] = back
		ctx.Data["Err_Password"] = true
		ctx.RenderWithErr(ctx.Tr("settings.reauth_wrong"), tplSettingsReauth, nil)
		return
	}
	if err := ctx.Session.Set(auth_model.ReauthSessionKey, int64(timeutil.TimeStampNow())); err != nil {
		ctx.ServerError("Session.Set", err)
		return
	}
	ctx.Flash.Success(ctx.Tr("settings.reauth_done"))
	ctx.Redirect(back)
}

// safeBack keeps the way back on this hub's settings: anything else returns to the security page.
func safeBack(to string) string {
	if to != "" && !httplib.IsRiskyRedirectURL(to) {
		return to
	}
	return setting.AppSubURL + "/user/settings/security"
}
