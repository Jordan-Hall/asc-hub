// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package auth

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"forgejo.org/modules/ascratelimit"
	"forgejo.org/modules/auth/password"
	"forgejo.org/modules/auth/password/pwn"
	"forgejo.org/modules/log"
	"forgejo.org/modules/setting"
	"forgejo.org/services/context"
)

// PasswordRange serves GET /-/password/range/{prefix}: the live rule "not found in breaches" of the strength meter
// (board Onboarding). The browser hashes the password with SHA-1 and sends only the first five hexadecimal digits;
// the answer is every breached hash suffix of that range, one per line, and the browser looks for its own. Neither the
// password nor its hash reaches the hub. The hub asks the Have-I-Been-Pwned range API with padding, so that service
// sees the prefix and the hub's address only. It exists only with [security] PASSWORD_CHECK_PWN, which also makes the
// hub check the password when the form is sent, and is limited per address.
func PasswordRange(ctx *context.Context) {
	if !setting.PasswordCheckPwn {
		ctx.NotFound("PasswordRange", nil)
		return
	}
	ctx.Resp.Header().Set("Cache-Control", "no-store")
	if !passwordRangeLimiter.Allow(ascratelimit.ClientKey(ctx.RemoteAddr())) {
		ctx.Resp.Header().Set("Retry-After", "60")
		ctx.PlainText(http.StatusTooManyRequests, "")
		return
	}
	suffixes, err := password.PwnedRange(ctx, ctx.Params("prefix"))
	switch {
	case errors.Is(err, pwn.ErrBadPrefix):
		ctx.PlainText(http.StatusBadRequest, "")
		return
	case err != nil:
		log.Warn("password range: %v", err)
		ctx.PlainText(http.StatusBadGateway, "")
		return
	}
	ctx.PlainText(http.StatusOK, strings.Join(suffixes, "\n"))
}

// passwordRangeLimiter allows thirty ranges a minute for one address: the meter asks once per pause in typing.
var passwordRangeLimiter = ascratelimit.New(30, time.Minute)
