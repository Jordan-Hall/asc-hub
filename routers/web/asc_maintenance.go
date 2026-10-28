// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package web

import (
	"net/http"
	"strings"

	"forgejo.org/services/ascmaintenance"
	"forgejo.org/services/context"
)

// ascMaintenanceOpen lists the writes that stay possible during maintenance, so an administrator can sign in and switch it off.
var ascMaintenanceOpen = []string{"/user/login", "/user/two_factor", "/user/webauthn", "/user/logout", "/user/recover_account", "/admin/maintenance"}

// ascMaintenanceState reads the state; tests replace it.
var ascMaintenanceState = func(ctx *context.Context) ascmaintenance.State { return ascmaintenance.Load(ctx) }

// ascMaintenanceGuard answers a change made through the browser with 503 while the hub is in maintenance. Reads go
// on, administrators are not stopped, and the page for signing in stays open.
func ascMaintenanceGuard(ctx *context.Context) {
	st := ascMaintenanceState(ctx)
	if !st.On {
		return
	}
	ctx.Data["AscMaintenance"] = st
	switch ctx.Req.Method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return
	}
	if ctx.Doer != nil && ctx.Doer.IsAdmin {
		return
	}
	for _, p := range ascMaintenanceOpen {
		if strings.HasPrefix(ctx.Req.URL.Path, p) {
			return
		}
	}
	ctx.Resp.Header().Set("Retry-After", "600")
	ctx.Data["IsRepo"] = false
	ctx.Error(http.StatusServiceUnavailable, st.Message)
}
