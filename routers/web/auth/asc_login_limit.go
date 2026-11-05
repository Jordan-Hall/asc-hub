// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package auth

import (
	"errors"
	"html/template"
	"net/http"
	"strconv"
	"time"

	"forgejo.org/modules/base"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/web/middleware"
	auth_method "forgejo.org/services/auth/method"
	"forgejo.org/services/context"
)

// The pages of the wait after wrong passwords (board Onboarding); the counting is auth_method.UserSignInFrom.

// ascWaitMinutes is a wait in whole minutes, rounded up, at least one.
func ascWaitMinutes(d time.Duration) int {
	return max(int((d+time.Minute-1)/time.Minute), 1)
}

// ascWrongPassword is the message of a wrong name or password, which says when the wait starts.
func ascWrongPassword(ctx *context.Context) template.HTML {
	if setting.LoginMaxAttempts == 0 {
		return ctx.Tr("form.username_password_incorrect")
	}
	m := ascWaitMinutes(setting.LoginWait)
	return ctx.Locale.TrN(m, "asc_auth.signin_wrong_1", "asc_auth.signin_wrong_n", setting.LoginMaxAttempts, m)
}

// ascRenderLoginWait answers a sign-in that must wait, when err is one: 429 with Retry-After, and the page naming the
// wait. It reports whether it answered.
func ascRenderLoginWait(ctx *context.Context, tpl base.TplName, form any, err error) bool {
	var w auth_method.ErrLoginWait
	if !errors.As(err, &w) {
		return false
	}
	m := ascWaitMinutes(w.Wait)
	ctx.Resp.Header().Set("Retry-After", strconv.Itoa(int((w.Wait+time.Second-1)/time.Second)))
	if form != nil {
		middleware.AssignForm(form, ctx.Data)
	}
	ctx.Flash.Error(ctx.Locale.TrN(m, "asc_auth.signin_wait_1", "asc_auth.signin_wait_n", m), true)
	ctx.HTML(http.StatusTooManyRequests, tpl)
	return true
}
