// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package admin

import (
	"net/http"
	"net/url"

	auth_model "forgejo.org/models/auth"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/json"
	"forgejo.org/modules/log"
	"forgejo.org/modules/setting"
	"forgejo.org/services/context"
)

// TestAuthSourcePost runs the Test sign-in of a password source: it checks the name and password and shows what the
// source says about the person, without signing anyone in or making an account.
func TestAuthSourcePost(ctx *context.Context) {
	source, err := auth_model.GetSourceByID(ctx, ctx.ParamsInt64(":authid"))
	if err != nil {
		ctx.ServerError("auth.GetSourceByID", err)
		return
	}
	prober, ok := source.Cfg.(auth_model.Prober)
	if !ok {
		ctx.Error(http.StatusBadRequest, "this source has no password test")
		return
	}
	login := ctx.FormTrim("login")
	ctx.Data["ProbeLogin"] = login
	ctx.Data["ProbeRan"] = true
	claims, err := prober.Probe(ctx, login, ctx.FormString("password"))
	switch {
	case user_model.IsErrUserNotExist(err) || user_model.IsErrUserProhibitLogin(err):
		ctx.Data["ProbeError"] = ctx.Locale.TrString("asc_admin.auth_test_refused")
	case err != nil:
		log.Info("admin test sign-in through %q: %v", source.Name, err)
		ctx.Data["ProbeError"] = err.Error()
	default:
		ctx.Data["ProbeClaims"] = claims
	}
	EditAuthSource(ctx)
}

// TestOAuthSource starts the Test sign-in of an OAuth source: the admin goes to the provider and, on the way back,
// sees the claims instead of being signed in.
func TestOAuthSource(ctx *context.Context) {
	source, err := auth_model.GetSourceByID(ctx, ctx.ParamsInt64(":authid"))
	if err != nil {
		ctx.ServerError("auth.GetSourceByID", err)
		return
	}
	if !source.IsOAuth2() || !source.IsActive {
		ctx.Error(http.StatusBadRequest, "only an active OAuth source has this test")
		return
	}
	if err := ctx.Session.Set(auth_model.ProbeOAuthSessionKey, source.ID); err != nil {
		ctx.ServerError("Session.Set", err)
		return
	}
	ctx.Redirect(setting.AppSubURL + "/user/oauth2/" + url.PathEscape(source.Name))
}

// takeOAuthProbe gives, once, the result of the OAuth test sign-in of this source.
func takeOAuthProbe(ctx *context.Context, sourceID int64) {
	raw, _ := ctx.Session.Get(auth_model.ProbeResultSessionKey).(string)
	if raw == "" {
		return
	}
	var res auth_model.ProbeResult
	if json.Unmarshal([]byte(raw), &res) != nil || res.SourceID != sourceID {
		return
	}
	_ = ctx.Session.Delete(auth_model.ProbeResultSessionKey)
	ctx.Data["ProbeRan"] = true
	if res.Error != "" {
		ctx.Data["ProbeError"] = res.Error
	} else {
		ctx.Data["ProbeClaims"] = res.Claims
	}
}
