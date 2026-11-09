// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	"net/http"
	"strings"
	"testing"

	issues_model "forgejo.org/models/issues"
	repo_model "forgejo.org/models/repo"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/templates"
	"forgejo.org/services/contexttest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// An agent assigned to an issue or a pull request carries the Agent chip in the sidebar, as in the list to pick from.
func TestAssigneesMarkAgents(t *testing.T) {
	bot := &user_model.User{ID: 77, Name: "agent-coder-2", LowerName: "agent-coder-2", Type: user_model.UserTypeBot}
	person := &user_model.User{ID: 78, Name: "alice", LowerName: "alice", Type: user_model.UserTypeIndividual}
	render := func(t *testing.T, existing bool, assigned []*user_model.User) string {
		t.Helper()
		ctx, resp := contexttest.MockContext(t, "/o/r", contexttest.MockContextOption{Render: templates.HTMLRenderer()})
		data := map[string]any{
			"Assignees":       []*user_model.User{bot, person},
			"Issue":           &issues_model.Issue{ID: 5, Index: 5, Assignees: assigned},
			"isExistingIssue": existing,
			"RepoLink":        "/o/r",
			"Repository":      &repo_model.Repository{ID: 1},
		}
		require.NoError(t, ctx.Render.HTML(resp, http.StatusOK, "repo/issue/view_content/sidebar/assignees", data, ctx.TemplateContext))
		return resp.Body.String()
	}

	html := render(t, true, []*user_model.User{bot})
	// once in the list to pick from and once among the assignees
	assert.Equal(t, 2, strings.Count(html, `data-testid="assignee-agent-mark"`))
	assert.Contains(t, html, "asc_rp.agent")

	assert.Equal(t, 1, strings.Count(render(t, true, []*user_model.User{person}), `data-testid="assignee-agent-mark"`), "the list only")
	// a new issue lists every candidate as a hidden row for the picker to show
	assert.Equal(t, 2, strings.Count(render(t, false, nil), `data-testid="assignee-agent-mark"`))
}
