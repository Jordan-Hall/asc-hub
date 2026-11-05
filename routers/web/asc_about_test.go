// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package web

import (
	gocontext "context"
	"net/http"
	"testing"

	"forgejo.org/models/unittest"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/templates"
	"forgejo.org/modules/test"
	"forgejo.org/modules/translation"
	"forgejo.org/services/contexttest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func renderAscAbout(t *testing.T) string {
	t.Helper()
	require.NoError(t, unittest.PrepareTestDatabase())
	if len(setting.Langs) == 0 || setting.Langs[0] != "en-US" {
		setting.Langs = []string{"en-US"}
		setting.Names = []string{"English"}
	}
	translation.InitLocales(gocontext.Background())
	ctx, resp := contexttest.MockContext(t, "GET /-/about")
	ctx.Render = templates.HTMLRenderer()
	ctx.Base.Locale = translation.NewLocale("en-US")
	if ctx.TemplateContext != nil {
		ctx.TemplateContext.Locale = ctx.Base.Locale
	}
	AscAbout(ctx)
	require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
	return resp.Body.String()
}

// Board Shell footer and board About: every footer link lands on a page or a section of the hub.
func TestAscFooterLinksLandOnHubPages(t *testing.T) {
	html := renderAscAbout(t)
	for _, href := range []string{
		`href="/-/docs/stacks-and-operations"`, `href="/-/docs/agents"`, `href="/-/docs#docs-new"`,
		`href="/-/docs/cli"`, `href="/-/docs/mcp"`, `href="/-/status"`,
		`href="/-/about#security"`, `href="/-/about#contact"`, `href="/-/about#privacy"`, `href="/-/about#terms"`,
		`href="/-/about#acceptable-use"`, `href="/-/about#subprocessors"`, `href="/-/about#cookies"`,
	} {
		assert.Contains(t, html, href)
	}
	for _, id := range []string{"security", "contact", "privacy", "terms", "acceptable-use", "subprocessors", "cookies"} {
		assert.Contains(t, html, `id="`+id+`"`, "the footer anchor #%s exists", id)
	}
	assert.NotContains(t, html, "github.com/Jordan-Hall/asc-hub/blob/", "the footer reads the docs of the hub, not the source")
}

func TestAscAboutContactsPoliciesAndCookies(t *testing.T) {
	html := renderAscAbout(t)
	assert.Contains(t, html, `href="https://github.com/Jordan-Hall/asc-hub/security/advisories/new"`, "without an address a report is private on the source")
	assert.Contains(t, html, `href="https://github.com/Jordan-Hall/asc-hub/security/policy"`)
	assert.NotContains(t, html, `data-testid="about-policies"`)
	assert.Contains(t, html, "<code>"+setting.SessionConfig.CookieName+"</code>")
	assert.Contains(t, html, "<code>"+setting.CookieRememberName+"</code>")
	assert.Contains(t, html, "Keeps you signed in during a visit")
	assert.Contains(t, html, "On this server", "local storage is not an outside service")

	defer test.MockVariableValue(&setting.Asc.ContactEmail, "hello@example.org")()
	defer test.MockVariableValue(&setting.Asc.SecurityEmail, "security@example.org")()
	defer test.MockVariableValue(&setting.Asc.PoliciesURL, "https://example.org/policies")()
	html = renderAscAbout(t)
	assert.Contains(t, html, `href="mailto:security@example.org"`)
	assert.Contains(t, html, `href="mailto:hello@example.org"`)
	assert.Contains(t, html, `href="https://example.org/policies"`)
	assert.NotContains(t, html, "security/advisories/new")
}

func TestAscAboutServicesNameOutsideStores(t *testing.T) {
	s3 := &setting.Storage{Type: setting.MinioStorageType, MinioConfig: setting.MinioStorageConfig{Endpoint: "s3.example.org:443"}}
	defer test.MockVariableValue(&setting.Attachment.Storage, s3)()
	defer test.MockVariableValue(&setting.LFS.Storage, s3)()
	defer test.MockVariableValue(&setting.MailService, &setting.Mailer{Protocol: "smtps", SMTPAddr: "smtp.example.org"})()
	got := ascAboutServices()
	assert.Equal(t, []AscAboutService{{Kind: "asc_site.about_sp_storage", Where: "s3.example.org"}, {Kind: "asc_site.about_sp_mail", Where: "smtp.example.org"}}, got, "one row per host")
}
