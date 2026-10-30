// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	"net/http"
	"testing"

	user_model "forgejo.org/models/user"
	"forgejo.org/modules/templates"
	"forgejo.org/services/contexttest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The agent mark follows an agent's account wherever an author is named (pull request, comment, review, timeline):
// the shared author link draws it for an account of type bot, and for no one else.
func TestAuthorLinkMarksAgents(t *testing.T) {
	render := func(t *testing.T, u *user_model.User) string {
		t.Helper()
		ctx, resp := contexttest.MockContext(t, "/o/r", contexttest.MockContextOption{Render: templates.HTMLRenderer()})
		require.NoError(t, ctx.Render.HTML(resp, http.StatusOK, "shared/user/authorlink", u, ctx.TemplateContext))
		return resp.Body.String()
	}
	bot := &user_model.User{ID: 77, Name: "agent-coder-2", LowerName: "agent-coder-2", Type: user_model.UserTypeBot}
	person := &user_model.User{ID: 78, Name: "alice", LowerName: "alice", Type: user_model.UserTypeIndividual}

	html := render(t, bot)
	assert.Contains(t, html, `data-testid="agent-mark"`)
	assert.Contains(t, html, "ds-chip-agent")
	assert.Contains(t, html, "agent-coder-2")
	assert.NotContains(t, render(t, person), "agent-mark")
}
