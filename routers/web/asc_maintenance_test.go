// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package web

import (
	"net/http"
	"testing"

	user_model "forgejo.org/models/user"
	"forgejo.org/services/ascmaintenance"
	"forgejo.org/services/context"
	"forgejo.org/services/contexttest"

	"github.com/stretchr/testify/assert"
)

func TestMaintenanceStopsChangesButNotReadingAdminsOrSignIn(t *testing.T) {
	prev := ascMaintenanceState
	t.Cleanup(func() { ascMaintenanceState = prev })
	on := ascmaintenance.State{On: true, Message: "Upgrading"}
	ascMaintenanceState = func(*context.Context) ascmaintenance.State { return on }

	call := func(method, path string, admin bool) int {
		ctx, resp := contexttest.MockContext(t, method+" "+path)
		ctx.Req.Method = method
		ctx.Req.Header.Set("Accept", "text/html")
		if admin {
			ctx.Doer = &user_model.User{ID: 1, IsAdmin: true}
		}
		ascMaintenanceGuard(ctx)
		if !ctx.Written() {
			return http.StatusOK
		}
		assert.Equal(t, "600", resp.Header().Get("Retry-After"))
		return resp.Code
	}
	assert.Equal(t, http.StatusServiceUnavailable, call("POST", "/user2/repo1/issues/new", false))
	assert.Equal(t, http.StatusServiceUnavailable, call("DELETE", "/user2/repo1/settings", false))
	assert.Equal(t, http.StatusOK, call("GET", "/user2/repo1", false), "reading goes on")
	assert.Equal(t, http.StatusOK, call("POST", "/user2/repo1/issues/new", true), "administrators are not stopped")
	assert.Equal(t, http.StatusOK, call("POST", "/user/login", false), "an administrator can sign in")
	assert.Equal(t, http.StatusOK, call("POST", "/admin/maintenance", false))

	on = ascmaintenance.State{}
	assert.Equal(t, http.StatusOK, call("POST", "/user2/repo1/issues/new", false), "off means off")
}
