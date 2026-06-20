// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package permissions

import (
	"net/http"

	"forgejo.org/modules/setting"
)

func ReqBasicOrRevProxyAuth(ctx Context) {
	if ctx.GetIsSigned() && setting.Service.EnableReverseProxyAuthAPI && ctx.GetAuthentication().IsReverseProxyAuthentication() {
		return
	}

	// Require basic authorization method to be used and that basic
	// authorization used password login to verify the user.
	if !ctx.GetAuthentication().IsPasswordAuthentication() {
		ctx.Error(http.StatusUnauthorized, "reqBasicAuth", "auth method not allowed")
		return
	}
}
