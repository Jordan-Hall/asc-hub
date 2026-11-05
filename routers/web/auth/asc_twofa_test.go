// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package auth

import (
	gocontext "context"
	"net/http"
	"testing"

	"forgejo.org/models/unittest"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/templates"
	"forgejo.org/modules/translation"
	"forgejo.org/services/context"
	"forgejo.org/services/contexttest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func renderSecondFactor(t *testing.T, path string, uid int64, serve func(*context.Context)) string {
	t.Helper()
	require.NoError(t, unittest.PrepareTestDatabase())
	if len(setting.Langs) == 0 || setting.Langs[0] != "en-US" {
		setting.Langs = []string{"en-US"}
		setting.Names = []string{"English"}
	}
	translation.InitLocales(gocontext.Background())
	ctx, resp := contexttest.MockContext(t, "GET "+path)
	ctx.Render = templates.HTMLRenderer()
	ctx.Base.Locale = translation.NewLocale("en-US")
	if ctx.TemplateContext != nil {
		ctx.TemplateContext.Locale = ctx.Base.Locale
	}
	ctx.Session = &ascIdeSession{values: map[any]any{"twofaUid": uid}}
	serve(ctx)
	require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
	return resp.Body.String()
}

// Board Onboarding, "Two-factor: authenticator code": a recovery code link, and "Use a security key instead" for a
// person who has a key.
func TestAscTwoFactorOffersTheSecurityKey(t *testing.T) {
	html := renderSecondFactor(t, "/user/two_factor", 24, TwoFactor)
	assert.Contains(t, html, "Use a recovery code")
	assert.NotContains(t, html, `data-testid="twofa-use-key"`, "user24 has no security key")

	html = renderSecondFactor(t, "/user/two_factor", 32, TwoFactor)
	assert.Contains(t, html, `href="/user/webauthn" data-testid="twofa-use-key"`)
	assert.Contains(t, html, "Use a security key instead")
}

// Board Onboarding, "Two-factor: security key": the waiting screen and "Try another way" with the ways that work for
// this person.
func TestAscWebAuthnTryAnotherWay(t *testing.T) {
	html := renderSecondFactor(t, "/user/webauthn", 32, WebAuthn)
	assert.Contains(t, html, `data-testid="webauthn-other"`)
	assert.Contains(t, html, "Try another way")
	assert.Contains(t, html, `href="/user/login"`)
	assert.NotContains(t, html, `href="/user/two_factor/scratch"`, "user32 has no authenticator, so no code or recovery code")

	html = renderSecondFactor(t, "/user/webauthn", 24, WebAuthn)
	assert.Contains(t, html, `href="/user/two_factor"`)
	assert.Contains(t, html, `href="/user/two_factor/scratch"`)
}
