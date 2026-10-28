// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package auth

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	auth_model "forgejo.org/models/auth"
	"forgejo.org/models/unittest"
	"forgejo.org/modules/json"
	"forgejo.org/services/contexttest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAscLimiter(t *testing.T) {
	now := time.Unix(1000, 0)
	l := newAscLimiter(3, time.Minute)
	l.now = func() time.Time { return now }
	for range 3 {
		assert.False(t, l.blocked("a"))
		l.add("a")
	}
	assert.True(t, l.blocked("a"))
	assert.False(t, l.blocked("b"), "keys are separate")
	now = now.Add(61 * time.Second)
	assert.False(t, l.blocked("a"), "the window passed")
}

func TestAscDeviceEndpoints(t *testing.T) {
	unittest.PrepareTestEnv(t)
	ascDeviceAsks = newAscLimiter(20, time.Minute)

	// the terminal asks for a code
	ctx, resp := contexttest.MockContext(t, "POST /login/device/code")
	ctx.Req.Form = url.Values{"client_name": {"maya-laptop"}}
	AscDeviceCode(ctx)
	require.Equal(t, http.StatusOK, resp.Code)
	var started struct {
		DeviceCode string `json:"device_code"`
		UserCode   string `json:"user_code"`
		Complete   string `json:"verification_uri_complete"`
		ExpiresIn  int    `json:"expires_in"`
		Interval   int    `json:"interval"`
	}
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &started))
	assert.Len(t, started.UserCode, 9)
	assert.Contains(t, started.Complete, "/login/device?user_code="+started.UserCode)
	assert.Equal(t, 600, started.ExpiresIn)
	assert.Equal(t, 5, started.Interval)
	assert.Equal(t, "no-store", resp.Header().Get("Cache-Control"))

	poll := func() (int, map[string]string) {
		ctx, resp := contexttest.MockContext(t, "POST /login/device/token")
		ctx.Req.Form = url.Values{"grant_type": {ascDeviceGrantType}, "device_code": {started.DeviceCode}}
		AscDeviceToken(ctx)
		var body map[string]string
		require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &body))
		return resp.Code, body
	}
	code, body := poll()
	assert.Equal(t, http.StatusBadRequest, code)
	assert.Equal(t, "authorization_pending", body["error"])

	// a person answers
	row, err := auth_model.GetAscDeviceCodeByUserCode(t.Context(), started.UserCode)
	require.NoError(t, err)
	require.NoError(t, row.Decide(t.Context(), 2, true))

	code, body = poll()
	assert.Equal(t, http.StatusOK, code)
	assert.Equal(t, "bearer", body["token_type"])
	assert.Len(t, body["access_token"], 40)

	// and the token is not handed out twice
	code, body = poll()
	assert.Equal(t, http.StatusBadRequest, code)
	assert.Equal(t, "invalid_grant", body["error"])
}

func TestAscDeviceTokenRejectsBadRequests(t *testing.T) {
	unittest.PrepareTestEnv(t)
	for name, form := range map[string]struct {
		form url.Values
		want string
	}{
		"wrong grant":    {url.Values{"grant_type": {"password"}, "device_code": {"x"}}, "unsupported_grant_type"},
		"no device code": {url.Values{"grant_type": {ascDeviceGrantType}}, "invalid_request"},
		"unknown code":   {url.Values{"grant_type": {ascDeviceGrantType}, "device_code": {"nope"}}, "invalid_grant"},
	} {
		t.Run(name, func(t *testing.T) {
			ctx, resp := contexttest.MockContext(t, "POST /login/device/token")
			ctx.Req.Form = form.form
			AscDeviceToken(ctx)
			assert.Equal(t, http.StatusBadRequest, resp.Code)
			assert.True(t, strings.Contains(resp.Body.String(), `"error":"`+form.want+`"`), resp.Body.String())
		})
	}
}

func TestAscDeviceCodeIsRateLimited(t *testing.T) {
	unittest.PrepareTestEnv(t)
	ascDeviceAsks = newAscLimiter(2, time.Minute)
	t.Cleanup(func() { ascDeviceAsks = newAscLimiter(20, time.Minute) })
	for i, want := range []int{http.StatusOK, http.StatusOK, http.StatusTooManyRequests} {
		ctx, resp := contexttest.MockContext(t, "POST /login/device/code")
		AscDeviceCode(ctx)
		assert.Equal(t, want, resp.Code, "request %d", i)
	}
}
