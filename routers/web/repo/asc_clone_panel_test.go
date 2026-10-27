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

// The clone panel of an asc repository offers the asc command, and the git command only while the git
// bridge exists (docs/asc/ASC-NATIVE-UI.md).
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

	t.Run("the bridge is on: both commands", func(t *testing.T) {
		html := render(t, true)
		assert.Contains(t, html, `asc clone <span class="js-clone-url">https://hub.example/o/r.git</span>`)
		assert.Contains(t, html, `git clone <span class="js-clone-url">https://hub.example/o/r.git</span>`)
		assert.Contains(t, html, `data-tab="asc-clone-with-git"`)
	})

	t.Run("the bridge is off: the asc command only", func(t *testing.T) {
		html := render(t, false)
		assert.Contains(t, html, `asc clone <span class="js-clone-url">`)
		assert.NotContains(t, html, "git clone")
		assert.NotContains(t, html, `data-tab="asc-clone-with-git"`)
		assert.NotContains(t, html, `tabular menu`, "one command needs no tabs")
	})
}
