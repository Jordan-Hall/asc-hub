// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	"net/http"
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
