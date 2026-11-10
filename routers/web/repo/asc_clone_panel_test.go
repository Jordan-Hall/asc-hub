// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	repo_model "forgejo.org/models/repo"
	"forgejo.org/modules/templates"
	"forgejo.org/services/contexttest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The git compatibility note of an asc repository (board GitNote) is one collapsed disclosure with the git
// command, and it exists only while the git bridge does (docs/asc/ASC-NATIVE-UI.md).
func TestAscClonePanel(t *testing.T) {
	render := func(t *testing.T, gitAccess bool) string {
		t.Helper()
		ctx, resp := contexttest.MockContext(t, "/o/r", contexttest.MockContextOption{Render: templates.HTMLRenderer()})
		ctx.Data["AscGitAccess"] = gitAccess
		ctx.Data["CloneButtonOriginLink"] = &repo_model.CloneLink{HTTPS: "https://hub.example/o/r.git", SSH: "ssh://git@hub.example/o/r.git"}
		ctx.HTML(http.StatusOK, "repo/clone_asc_tabs")
		require.Equal(t, http.StatusOK, resp.Code)
		return resp.Body.String()
	}

	t.Run("the bridge is on: a collapsed note with the git command", func(t *testing.T) {
		html := render(t, true)
		assert.Contains(t, html, `<details class="asc-git-compat`)
		assert.NotContains(t, html, `<details class="asc-git-compat" open`)
		assert.Contains(t, html, `git clone <span class="js-clone-url">https://hub.example/o/r.git</span>`)
	})

	t.Run("the bridge is off: no git command", func(t *testing.T) {
		html := render(t, false)
		assert.NotContains(t, html, "git clone")
		assert.NotContains(t, html, "asc-git-compat")
	})
}

// Sign-in is per repository and on demand (docs/asc/ASC-NATIVE-UI.md): the clone sheet shows only `asc clone` and
// says that a private repository asks for a device code; no repository page asks for an up-front `asc auth login`.
func TestAscCloneNeedsNoSignInStep(t *testing.T) {
	ctx, resp := contexttest.MockContext(t, "/o/r", contexttest.MockContextOption{Render: templates.HTMLRenderer()})
	ctx.Data["Repository"] = &repo_model.Repository{OwnerName: "o", Name: "r", StorageBackend: "asc"}
	ctx.Data["CloneButtonShowHTTPS"] = true
	ctx.Data["CloneButtonOriginLink"] = &repo_model.CloneLink{HTTPS: "https://hub.example/o/r.git"}
	ctx.HTML(http.StatusOK, "repo/clone_dialog")
	require.Equal(t, http.StatusOK, resp.Code)
	html := resp.Body.String()
	assert.Contains(t, html, `asc clone <span class="js-clone-url">https://hub.example/o/r.git</span>`)
	assert.NotContains(t, html, "auth login")
	assert.Contains(t, html, `data-testid="clone-signin-help">repo.clone_signin_help</p>`)
	locale, err := os.ReadFile(filepath.Join("..", "..", "..", "options", "locale", "locale_en-US.ini"))
	require.NoError(t, err)
	assert.Contains(t, string(locale), "\nclone_signin_help = There is no sign-in step. A private repository asks for a device code in the terminal, and you approve it on this site. A public repository needs nothing.\n")

	for _, name := range []string{"clone_dialog", "clone_asc_tabs", "empty"} {
		src, err := os.ReadFile(filepath.Join("..", "..", "..", "templates", "repo", name+".tmpl"))
		require.NoError(t, err)
		assert.NotContains(t, string(src), "asc auth login", name)
	}
}

// The GitNote board notes: the whole row is the summary of one disclosure (a native one: Enter and Space open it, and
// it reports whether it is open), with nothing else to press in it; the address and its copy button are inside the
// body, so they are reachable only after opening; switched off, one sentence with nothing to press; the note is inline
// on the repository home card, the empty repository and Working mode; its colours are theme tokens only.
func TestAscGitNoteBoardNotes(t *testing.T) {
	render := func(t *testing.T, gitAccess bool) string {
		t.Helper()
		ctx, resp := contexttest.MockContext(t, "/o/r", contexttest.MockContextOption{Render: templates.HTMLRenderer()})
		ctx.Data["AscGitAccess"] = gitAccess
		ctx.Data["CloneButtonOriginLink"] = &repo_model.CloneLink{HTTPS: "https://hub.example/o/r.git"}
		ctx.HTML(http.StatusOK, "repo/clone_asc_tabs")
		require.Equal(t, http.StatusOK, resp.Code)
		return resp.Body.String()
	}

	html := render(t, true)
	summary, body, ok := strings.Cut(html, "</summary>")
	require.True(t, ok)
	summary = summary[strings.Index(summary, "<summary>"):]
	assert.Contains(t, summary, "asc_rp.gitnote_lead")
	assert.Contains(t, summary, "asc_rp.gitnote_toggle")
	for _, tag := range []string{"<a ", "<button", "<input"} {
		assert.NotContains(t, summary, tag, "the row is one button")
	}
	assert.NotContains(t, summary, "hub.example", "the address is not in the collapsed row")
	assert.Contains(t, body, `class="ds-copy" data-clipboard-target="#rp-git-clone-url"`)
	assert.Contains(t, body, `id="rp-git-clone-url"`)
	assert.Contains(t, body, `readonly tabindex="-1" aria-hidden="true"`, "the copy source itself is not a stop of its own")

	off := render(t, false)
	assert.Equal(t, 1, strings.Count(off, "asc_rp.gitnote_off"))
	for _, tag := range []string{"<a ", "<button", "<details", "<input"} {
		assert.NotContains(t, off, tag, "switched off: nothing to press")
	}
	locale, err := os.ReadFile(filepath.Join("..", "..", "..", "options", "locale", "locale_en-US.ini"))
	require.NoError(t, err)
	assert.Contains(t, string(locale), "\ngitnote_off = The Git remote is off on this hub. Everything works with asc.\n")

	for _, name := range []string{"home_cards", "empty", "settings/asc_branchless"} {
		src, err := os.ReadFile(filepath.Join("..", "..", "..", "templates", "repo", name+".tmpl"))
		require.NoError(t, err)
		assert.Contains(t, string(src), `{{template "repo/clone_asc_tabs"`, name)
	}

	css, err := os.ReadFile(filepath.Join("..", "..", "..", "web_src", "css", "asc-pages", "repo-home.css"))
	require.NoError(t, err)
	_, rules, ok := strings.Cut(string(css), "/* ---- git compatibility note ---- */")
	require.True(t, ok)
	rules, _, _ = strings.Cut(rules, "/* ---- ")
	for _, line := range strings.Split(rules, "\n") {
		if strings.Contains(line, "color") || strings.Contains(line, "background") || strings.Contains(line, "border") {
			assert.NotRegexp(t, `#[0-9a-fA-F]{3,8}\b|rgb|hsl`, line, "dark and light come from the theme tokens")
		}
	}
}
