// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package web

import (
	gocontext "context"
	"net/http"
	"testing"

	"forgejo.org/models/unittest"
	"forgejo.org/modules/ascdownload"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/templates"
	"forgejo.org/modules/test"
	"forgejo.org/modules/translation"
	"forgejo.org/services/context"
	"forgejo.org/services/contexttest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func renderAscAbout(t *testing.T) string {
	t.Helper()
	return renderAscPublic(t, "/-/about", func(ctx *context.Context) { AscAbout(ctx) })
}

func renderAscPublic(t *testing.T, path string, serve func(ctx *context.Context)) string {
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
	serve(ctx)
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

func TestAscAboutServicesNeverShowConfiguredHosts(t *testing.T) {
	s3 := &setting.Storage{Type: setting.MinioStorageType, MinioConfig: setting.MinioStorageConfig{Endpoint: "10.0.0.5:9000"}}
	defer test.MockVariableValue(&setting.Attachment.Storage, s3)()
	defer test.MockVariableValue(&setting.MailService, &setting.Mailer{Protocol: "smtps", SMTPAddr: "mail.internal.example.org"})()
	defer test.MockVariableValue(&setting.Asc.Subprocessors, []setting.AscSubprocessor{{Name: "Object Store Co", Purpose: "Files and backups"}})()
	got := ascAboutServices()
	assert.Equal(t, []AscAboutService{
		{Name: "Object Store Co", Purpose: "Files and backups"},
		{Kind: "asc_site.about_sp_storage"},
		{Kind: "asc_site.about_sp_mail"},
	}, got)

	html := renderAscAbout(t)
	assert.Contains(t, html, "Object Store Co")
	assert.Contains(t, html, "With an outside provider the operator chose")
	assert.NotContains(t, html, "10.0.0.5")
	assert.NotContains(t, html, "mail.internal.example.org")
}

// Board Main, hero pill: the announcement names the version of the release the hub offers, and no version without one.
func TestAscLandingPillNamesTheRealRelease(t *testing.T) {
	home := func(view *ascDownloadView) string {
		return renderAscPublic(t, "/", func(ctx *context.Context) {
			if view != nil {
				ctx.Data["AscDownload"] = view
			}
			ctx.HTML(http.StatusOK, tplHome)
		})
	}
	html := home(nil)
	assert.Contains(t, html, "Agent runs and the merge queue are here")
	assert.NotContains(t, html, "ASC 1.4")

	html = home(&ascDownloadView{Offers: &ascdownload.Offers{Manifest: &ascdownload.Manifest{Version: "0.1.3"}}})
	assert.Contains(t, html, "ASC 0.1.3: agent runs and the merge queue are here")
}
