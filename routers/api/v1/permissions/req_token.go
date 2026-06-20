// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package permissions

import (
	"net/http"
)

func ReqToken(ctx Context) {
	// If actions token is present
	if ctx.GetAuthentication().ActionsTaskID().Has() {
		return
	}

	if ctx.GetIsSigned() {
		return
	}
	ctx.Error(http.StatusUnauthorized, "reqToken", "token is required")
}
