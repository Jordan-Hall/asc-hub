// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package auth

import (
	"net/http"
	"testing"

	"forgejo.org/models/unittest"
	"forgejo.org/modules/json"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/test"
	"forgejo.org/services/contexttest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSignUpNameAvailable(t *testing.T) {
	unittest.PrepareTestEnv(t)
	defer test.MockVariableValue(&setting.Service.DisableRegistration, false)()
	defer test.MockVariableValue(&setting.Service.AllowOnlyExternalRegistration, false)()

	state := func(name string) string {
		ctx, resp := contexttest.MockContext(t, "GET /user/sign_up/name?name="+name)
		ctx.Req.Form = map[string][]string{"name": {name}}
		SignUpNameAvailable(ctx)
		require.Equal(t, http.StatusOK, resp.Code)
		var out map[string]string
		require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &out))
		return out["state"]
	}
	assert.Equal(t, "ok", state("brand-new-person"))
	assert.Equal(t, "taken", state("user2"))
	assert.Equal(t, "taken", state("USER2"), "names are not case sensitive")
	assert.Equal(t, "reserved", state("admin"))
	assert.Equal(t, "invalid", state("no spaces"))
	assert.Equal(t, "invalid", state(""))
	assert.Equal(t, "invalid", state("a-very-long-name-that-is-longer-than-forty-characters"))

	// where registration is closed the route does not exist
	defer test.MockVariableValue(&setting.Service.DisableRegistration, true)()
	ctx, resp := contexttest.MockContext(t, "GET /user/sign_up/name")
	SignUpNameAvailable(ctx)
	assert.Equal(t, http.StatusNotFound, resp.Code)
}
