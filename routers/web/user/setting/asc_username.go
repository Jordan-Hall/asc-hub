// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package setting

import (
	"net/http"

	"forgejo.org/models/db"
	user_model "forgejo.org/models/user"
	"forgejo.org/services/context"
	user_service "forgejo.org/services/user"
)

// ascUsernameAnswer is the answer of UsernameCheck.
type ascUsernameAnswer struct {
	OK      bool   `json:"ok"`
	Message string `json:"message,omitempty"`
}

// UsernameCheck serves `GET /user/settings/username_check?name=`: whether the signed-in person could take that name. It is
// advice for the profile page (the form says the same after submit); it asks the same questions as a rename, so a name that
// is free, usable and not in its cooldown is the only one it blesses. Only a signed-in person can ask.
func UsernameCheck(ctx *context.Context) {
	ctx.Resp.Header().Set("Cache-Control", "no-store")
	name := ctx.FormTrim("name")
	say := func(key string, args ...any) {
		ctx.JSON(http.StatusOK, ascUsernameAnswer{Message: ctx.Locale.TrString(key, args...)})
	}
	switch {
	case name == "" || len(name) > 40:
		ctx.JSON(http.StatusOK, ascUsernameAnswer{})
		return
	case name == ctx.Doer.Name:
		ctx.JSON(http.StatusOK, ascUsernameAnswer{OK: true})
		return
	}
	if ok, err := user_service.CanUserRename(ctx, ctx.Doer); err != nil {
		ctx.ServerError("CanUserRename", err)
		return
	} else if !ok {
		say("settings.password_username_disabled")
		return
	}
	if err := user_model.IsUsableUsername(name); err != nil {
		switch {
		case db.IsErrNameReserved(err):
			say("user.form.name_reserved", name)
		case db.IsErrNamePatternNotAllowed(err):
			say("user.form.name_pattern_not_allowed", name)
		default:
			say("asc_work.sp_username_chars")
		}
		return
	}
	if exists, err := user_model.IsUserExist(ctx, ctx.Doer.ID, name); err != nil {
		ctx.ServerError("IsUserExist", err)
		return
	} else if exists {
		say("form.username_been_taken")
		return
	}
	if can, _, err := user_model.CanClaimUsername(ctx, name, ctx.Doer.ID); err != nil {
		ctx.ServerError("CanClaimUsername", err)
		return
	} else if !can {
		say("asc_work.sp_username_cooldown")
		return
	}
	ctx.JSON(http.StatusOK, ascUsernameAnswer{OK: true})
}
