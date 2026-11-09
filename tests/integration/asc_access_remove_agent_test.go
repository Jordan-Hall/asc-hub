// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package integration

import (
	"fmt"
	"net/http"
	"net/url"
	"testing"

	"forgejo.org/models/db"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"
	repo_module "forgejo.org/modules/repository"
	"forgejo.org/services/ascagentui"
	app_context "forgejo.org/services/context"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Removing an agent from the access page asks with its own confirm (its running run stops, its pull requests stay
// open) and removes it; a git repository has no agent runs to stop, so nothing else happens.
func TestAscAccessRemoveAgent(t *testing.T) {
	onApplicationRun(t, func(t *testing.T, _ *url.URL) {
		repo := unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{ID: 1})
		bot, err := ascagentui.EnsureAgentUser(db.DefaultContext, "fixer")
		require.NoError(t, err)
		require.NoError(t, repo_module.AddCollaborator(db.DefaultContext, repo, bot))
		require.NoError(t, repo_module.AddCollaborator(db.DefaultContext, repo, unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 4})))

		session := loginUser(t, "user2")
		resp := session.MakeRequest(t, NewRequest(t, "GET", "/user2/repo1/settings/collaboration"), http.StatusOK)
		doc := NewHTMLParser(t, resp.Body)
		button := doc.Find(fmt.Sprintf(`[data-access-row="agent"] .delete-button[data-id="%d"]`, bot.ID))
		modal, _ := button.Attr("data-modal-id")
		assert.Equal(t, "delete-agent", modal)
		assert.Contains(t, doc.Find("#delete-agent").Text(), "A run of this agent that is going now stops.")
		assert.Contains(t, doc.Find("#delete-agent").Text(), "Pull requests it already opened stay open")
		person, _ := doc.Find(`[data-access-row="person"] .delete-button`).First().Attr("data-modal-id")
		assert.Equal(t, "delete-collaborator", person, "a person keeps the plain confirm")

		session.MakeRequest(t, NewRequestWithValues(t, "POST", "/user2/repo1/settings/collaboration/delete", map[string]string{
			"id": fmt.Sprint(bot.ID),
		}), http.StatusOK)
		unittest.AssertNotExistsBean(t, &repo_model.Collaboration{RepoID: repo.ID, UserID: bot.ID})
		flash := session.GetCookie(app_context.CookieNameFlash)
		if assert.NotNil(t, flash) {
			assert.Contains(t, flash.Value, "success")
			assert.NotContains(t, flash.Value, "warning", "nothing to stop on a git repository")
		}
	})
}
