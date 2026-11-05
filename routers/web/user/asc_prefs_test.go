// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package user

import (
	"net/http"
	"net/url"
	"testing"

	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"
	"forgejo.org/services/contexttest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func ascPostPrefs(t *testing.T, form url.Values) int {
	t.Helper()
	ctx, _ := contexttest.MockContext(t, "POST /-/asc/prefs")
	contexttest.LoadUser(t, ctx, 2)
	ctx.Req.PostForm = form
	ctx.Req.Form = form
	AscPrefsPost(ctx)
	return ctx.Resp.Status()
}

func TestAscPrefsPost(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	ctx := t.Context()

	assert.Equal(t, http.StatusNoContent, ascPostPrefs(t, url.Values{"theme": {"dark"}}))
	assert.Equal(t, "asc-dark", unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2}).Theme)

	assert.Equal(t, http.StatusNoContent, ascPostPrefs(t, url.Values{"density": {"compact"}}))
	assert.Equal(t, user_model.AscDensityCompact, user_model.AscDensity(ctx, 2))

	assert.Equal(t, http.StatusNoContent, ascPostPrefs(t, url.Values{"theme": {"system"}, "density": {"comfy"}}))
	assert.Equal(t, "asc-auto", unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2}).Theme)
	assert.Equal(t, user_model.AscDensityComfy, user_model.AscDensity(ctx, 2))
}

func TestAscPrefsPostRefused(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	ctx := t.Context()
	before := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2}).Theme

	for _, form := range []url.Values{
		{},
		{"theme": {"forgejo-dark"}},
		{"theme": {"sepia"}},
		{"density": {"huge"}},
		{"theme": {"light"}, "density": {"huge"}},
	} {
		assert.Equal(t, http.StatusBadRequest, ascPostPrefs(t, form), form.Encode())
	}
	assert.Equal(t, before, unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2}).Theme, "nothing saved, not even the valid half")
	assert.Equal(t, user_model.AscDensityComfy, user_model.AscDensity(ctx, 2))
}

func TestAscPrefsHead(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	require.NoError(t, user_model.SetAscDensity(t.Context(), 2, user_model.AscDensityCompact))

	rctx, body := ascRenderContext(t, "/-/asc/inbox", 2)
	rctx.HTML(http.StatusOK, "base/head")
	html := string(body())
	assert.Contains(t, html, `<meta name="asc-prefs" content="/-/asc/prefs" data-failed="Could not save this choice to your account. Try again.">`)
	assert.Contains(t, html, `var signed = true;`)
	assert.Contains(t, html, `var accountDensity = "compact";`)

	rctx, body = ascRenderContext(t, "/-/asc/inbox", 0)
	rctx.HTML(http.StatusOK, "base/head")
	html = string(body())
	assert.NotContains(t, html, `name="asc-prefs"`, "a guest keeps the choice in the browser")
	assert.Contains(t, html, `var signed = false;`)
	assert.Contains(t, html, `var accountDensity = "";`)
}
