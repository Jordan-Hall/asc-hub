// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package auth

import (
	"net/http"
	"strings"

	"forgejo.org/models/db"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/setting"
	"forgejo.org/services/context"
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
// invalid. It does not exist where registration is closed.
func SignUpNameAvailable(ctx *context.Context) {
	if setting.Service.DisableRegistration || setting.Service.AllowOnlyExternalRegistration {
		ctx.NotFound("SignUpNameAvailable", nil)
		return
	}
	ctx.Resp.Header().Set("Cache-Control", "no-store")
	ctx.JSON(http.StatusOK, map[string]string{"state": signUpNameState(ctx, strings.TrimSpace(ctx.FormString("name")))})
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
	used, err := user_model.IsUserExist(ctx, 0, name)
	if err != nil {
		return signUpNameInvalid
	}
	if used {
		return signUpNameTaken
	}
	return signUpNameOK
}
