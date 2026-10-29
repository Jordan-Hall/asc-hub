// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package auth

import (
	"net/http"
	"strings"
	"time"

	"forgejo.org/models/db"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/ascratelimit"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/structs"
	"forgejo.org/services/context"
	"forgejo.org/services/forms"
)

// States of the answer of SignUpNameAvailable.
const (
	signUpNameOK       = "ok"
	signUpNameTaken    = "taken"
	signUpNameInvalid  = "invalid"
	signUpNameReserved = "reserved"
)

// SignUpNameAvailable serves GET /user/sign_up/name?name=...: whether a user name can be taken, for the live hint under the
// user name field of the sign up form. It says no more than the sign up itself would on a post: ok, taken, reserved or
// invalid. It does not exist where registration is closed, where a captcha guards the sign up, where the site asks for a
// sign in to view, or where new accounts are not public by default: the answer would tell a stranger which names are
// in use. Where it exists it is limited per address, and a private or limited account reads like a free name. The sign
// up post stays the authority.
func SignUpNameAvailable(ctx *context.Context) {
	if !signUpNameEndpointOpen() {
		ctx.NotFound("SignUpNameAvailable", nil)
		return
	}
	ctx.Resp.Header().Set("Cache-Control", "no-store")
	if !signUpNameLimiter.Allow(ascratelimit.ClientKey(ctx.RemoteAddr())) {
		ctx.Resp.Header().Set("Retry-After", "60")
		ctx.JSON(http.StatusTooManyRequests, map[string]string{"state": signUpNameInvalid})
		return
	}
	ctx.JSON(http.StatusOK, map[string]string{"state": signUpNameState(ctx, strings.TrimSpace(ctx.FormString("name")))})
}

// signUpNameLimiter allows twenty checks a minute for one address.
var signUpNameLimiter = ascratelimit.New(20, time.Minute)

func signUpNameEndpointOpen() bool {
	switch {
	case setting.Service.DisableRegistration, setting.Service.AllowOnlyExternalRegistration:
		return false
	case setting.Service.EnableCaptcha, setting.Service.RequireSignInView, setting.Service.Explore.RequireSigninView:
		return false
	}
	return setting.Service.DefaultUserVisibilityMode == structs.VisibleTypePublic
}

func signUpNameState(ctx *context.Context, name string) string {
	if name == "" || len(name) > 40 {
		return signUpNameInvalid
	}
	if err := user_model.IsUsableUsername(name); err != nil {
		if db.IsErrNameReserved(err) || db.IsErrNamePatternNotAllowed(err) {
			return signUpNameReserved
		}
		return signUpNameInvalid
	}
	// One query for a free and for a taken name. Only a public account counts as taken: a private or a limited one reads
	// like a free name, so the check does not list who is on the site. The sign up post gives the final answer.
	u, err := user_model.GetUserByName(ctx, name)
	if err != nil && !user_model.IsErrUserNotExist(err) {
		return signUpNameInvalid
	}
	if err == nil && u.Visibility == structs.VisibleTypePublic {
		return signUpNameTaken
	}
	return signUpNameOK
}

// newSignUpUser is the account a sign up form asks for: the user name, the optional full name, the address and the password.
func newSignUpUser(form *forms.RegisterForm) *user_model.User {
	return &user_model.User{
		Name:     form.UserName,
		FullName: strings.TrimSpace(form.FullName),
		Email:    form.Email,
		Passwd:   form.Password,
	}
}
