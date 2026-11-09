// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	"net/http"
	"os"
	"path/filepath"
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
