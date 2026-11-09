// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package auth

import (
	"net/url"
	"strings"

	"forgejo.org/modules/eventsource"
	"forgejo.org/modules/httplib"
	"forgejo.org/modules/setting"
	"forgejo.org/services/context"
)

// AscSwitchAccount signs the person out and opens the sign-in page, which goes back to redirect_to (a path of this
// hub) once another account signed in. It is the Switch account action of the 403 page.
func AscSwitchAccount(ctx *context.Context) {
	to := ctx.FormString("redirect_to")
	if !strings.HasPrefix(to, "/") || httplib.IsRiskyRedirectURL(to) {
		to = ""
	}
	if ctx.Doer != nil {
		eventsource.GetManager().SendMessage(ctx.Doer.ID, &eventsource.Event{Name: "logout", Data: ctx.Session.ID()})
	}
	HandleSignOut(ctx)
	login := setting.AppSubURL + "/user/login"
	if to != "" {
		login += "?redirect_to=" + url.QueryEscape(to)
	}
	ctx.Redirect(login)
}
