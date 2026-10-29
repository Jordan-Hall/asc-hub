// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package web

import (
	"net/http"
	"testing"
	"time"

	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/ascratelimit"
	"forgejo.org/modules/json"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/test"
	"forgejo.org/services/contexttest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func paletteGroups(t *testing.T, q string, doer *user_model.User) (int, map[string]bool) {
	t.Helper()
	ctx, resp := contexttest.MockContext(t, "GET /-/asc/palette?q="+q)
	ctx.Req.Form = map[string][]string{"q": {q}}
	if doer != nil {
		contexttest.LoadUser(t, ctx, doer.ID)
		ctx.IsSigned = true
	}
	AscPalette(ctx)
	var out struct {
		Groups []AscPaletteGroup `json:"groups"`
	}
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &out))
	keys := map[string]bool{}
	for _, g := range out.Groups {
		keys[g.Key] = true
	}
	return resp.Code, keys
}

func TestPaletteShowsPeopleOnlyWhereTheExplorePageDoes(t *testing.T) {
	unittest.PrepareTestEnv(t)
	ascPaletteLimiter = ascratelimit.New(1000, time.Second)

	code, keys := paletteGroups(t, "user", nil)
	assert.Equal(t, http.StatusOK, code)
	assert.True(t, keys["people"], "a visitor finds public people")

	t.Run("users page disabled", func(t *testing.T) {
		defer test.MockVariableValue(&setting.Service.Explore.DisableUsersPage, true)()
		_, keys := paletteGroups(t, "user", nil)
		assert.False(t, keys["people"])
		doer := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2})
		_, keys = paletteGroups(t, "user", doer)
		assert.False(t, keys["people"], "also for a signed in person")
	})
	t.Run("explore needs a sign in", func(t *testing.T) {
		defer test.MockVariableValue(&setting.Service.Explore.RequireSigninView, true)()
		_, keys := paletteGroups(t, "user", nil)
		assert.False(t, keys["people"])
		assert.False(t, keys["repos"])
		doer := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2})
		_, keys = paletteGroups(t, "user", doer)
		assert.True(t, keys["people"], "a signed in person may search")
	})
}

func TestPaletteHidesPrivateAndLimitedPeopleFromAVisitor(t *testing.T) {
	unittest.PrepareTestEnv(t)
	ascPaletteLimiter = ascratelimit.New(1000, time.Second)
	ctx, resp := contexttest.MockContext(t, "GET /-/asc/palette?q=user31")
	ctx.Req.Form = map[string][]string{"q": {"user31"}}
	AscPalette(ctx)
	assert.NotContains(t, resp.Body.String(), "user31", "user31 is a private account")
	ctx, resp = contexttest.MockContext(t, "GET /-/asc/palette?q=user33")
	ctx.Req.Form = map[string][]string{"q": {"user33"}}
	AscPalette(ctx)
	assert.NotContains(t, resp.Body.String(), "user33", "user33 is a limited account")
}

func TestPaletteIsLimitedAndCapped(t *testing.T) {
	unittest.PrepareTestEnv(t)
	ascPaletteLimiter = ascratelimit.New(3, time.Minute)
	for i := 0; i < 3; i++ {
		code, _ := paletteGroups(t, "user", nil)
		assert.Equal(t, http.StatusOK, code)
	}
	code, keys := paletteGroups(t, "user", nil)
	assert.Equal(t, http.StatusTooManyRequests, code)
	assert.Empty(t, keys, "a refused call shows nothing")

	ascPaletteLimiter = ascratelimit.New(1000, time.Minute)
	long := make([]byte, ascPaletteMaxQuery+1)
	for i := range long {
		long[i] = 'u'
	}
	_, keys = paletteGroups(t, string(long), nil)
	assert.Empty(t, keys, "a long query is not searched")
}
