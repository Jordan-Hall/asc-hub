// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package auth

import (
	"net/http"
	"testing"
	"time"

	"forgejo.org/models/unittest"
	"forgejo.org/modules/ascratelimit"
	"forgejo.org/modules/json"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/structs"
	"forgejo.org/modules/test"
	"forgejo.org/services/contexttest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSignUpNameAvailable(t *testing.T) {
	unittest.PrepareTestEnv(t)
	signUpNameLimiter = ascratelimit.New(1000, time.Minute)
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

func nameState(t *testing.T, name string) (int, string) {
	t.Helper()
	ctx, resp := contexttest.MockContext(t, "GET /user/sign_up/name?name="+name)
	ctx.Req.Form = map[string][]string{"name": {name}}
	SignUpNameAvailable(ctx)
	var out map[string]string
	if resp.Code == http.StatusOK {
		require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &out))
	}
	return resp.Code, out["state"]
}

func TestSignUpNameDoesNotRevealPrivateAccounts(t *testing.T) {
	unittest.PrepareTestEnv(t)
	defer test.MockVariableValue(&setting.Service.DisableRegistration, false)()
	defer test.MockVariableValue(&setting.Service.AllowOnlyExternalRegistration, false)()
	signUpNameLimiter = ascratelimit.New(1000, time.Minute)

	for _, name := range []string{"user31", "user33", "privated_org", "limited_org"} {
		code, state := nameState(t, name)
		assert.Equal(t, http.StatusOK, code)
		assert.Equal(t, "ok", state, "%s is not public: it reads like a free name", name)
	}
	_, state := nameState(t, "user2")
	assert.Equal(t, "taken", state, "a public account is shown as taken, as the sign up page shows it too")
}

func TestSignUpNameIsClosedWhereItWouldLeakNames(t *testing.T) {
	unittest.PrepareTestEnv(t)
	defer test.MockVariableValue(&setting.Service.DisableRegistration, false)()
	defer test.MockVariableValue(&setting.Service.AllowOnlyExternalRegistration, false)()
	signUpNameLimiter = ascratelimit.New(1000, time.Minute)

	t.Run("captcha", func(t *testing.T) {
		defer test.MockVariableValue(&setting.Service.EnableCaptcha, true)()
		code, _ := nameState(t, "user2")
		assert.Equal(t, http.StatusNotFound, code)
	})
	t.Run("sign in to view", func(t *testing.T) {
		defer test.MockVariableValue(&setting.Service.RequireSignInView, true)()
		code, _ := nameState(t, "user2")
		assert.Equal(t, http.StatusNotFound, code)
	})
	t.Run("explore needs a sign in", func(t *testing.T) {
		defer test.MockVariableValue(&setting.Service.Explore.RequireSigninView, true)()
		code, _ := nameState(t, "user2")
		assert.Equal(t, http.StatusNotFound, code)
	})
	t.Run("new accounts are not public", func(t *testing.T) {
		defer test.MockVariableValue(&setting.Service.DefaultUserVisibilityMode, structs.VisibleTypeLimited)()
		code, _ := nameState(t, "user2")
		assert.Equal(t, http.StatusNotFound, code)
	})
}

func TestSignUpNameIsLimitedPerAddress(t *testing.T) {
	unittest.PrepareTestEnv(t)
	defer test.MockVariableValue(&setting.Service.DisableRegistration, false)()
	defer test.MockVariableValue(&setting.Service.AllowOnlyExternalRegistration, false)()
	signUpNameLimiter = ascratelimit.New(3, time.Minute)

	for i := 0; i < 3; i++ {
		code, _ := nameState(t, "someone-new")
		assert.Equal(t, http.StatusOK, code)
	}
	code, _ := nameState(t, "someone-new")
	assert.Equal(t, http.StatusTooManyRequests, code)
}
