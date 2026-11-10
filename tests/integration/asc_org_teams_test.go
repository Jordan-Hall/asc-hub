// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package integration

import (
	"net/http"
	"strings"
	"testing"

	"forgejo.org/models/db"
	org_model "forgejo.org/models/organization"
	"forgejo.org/models/perm"
	"forgejo.org/tests"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The OrgTeams board states: the selected team below the cards, an empty team that prompts to add a member, a team
// without repositories that prompts to add one, and deleting a team behind a confirmation.
func TestAscOrgTeamsBoardNotes(t *testing.T) {
	defer tests.PrepareTestEnv(t)()
	require.NoError(t, db.Insert(db.DefaultContext, &org_model.Team{OrgID: 3, LowerName: "quiet", Name: "quiet", AccessMode: perm.AccessModeRead}))

	owner := loginUser(t, "user2")
	page := func(team string) *HTMLDoc {
		resp := owner.MakeRequest(t, NewRequest(t, "GET", "/org/org3/teams?team="+team), http.StatusOK)
		return NewHTMLParser(t, resp.Body)
	}

	t.Run("default shows the selected team below the cards", func(t *testing.T) {
		doc := page("team1")
		assert.Equal(t, "team1", strings.TrimSpace(doc.Find(`[data-testid="org-team-detail"] h2`).Text()))
		assert.Equal(t, "true", doc.Find(`.wk-team-card.is-selected .wk-team-pick-link`).AttrOr("aria-current", ""))
		assert.Equal(t, 0, doc.Find(`[data-testid="org-team-no-members"]`).Length(), "team1 has members")
		assert.Equal(t, 0, doc.Find(`[data-testid="org-team-no-repos"]`).Length(), "team1 has repositories")
	})

	t.Run("an empty team prompts to add a member and a repository", func(t *testing.T) {
		doc := page("quiet")
		members := doc.Find(`[data-testid="org-team-no-members"]`)
		require.Equal(t, 1, members.Length())
		assert.Contains(t, members.Text(), "No one is in this team yet.")
		assert.Equal(t, "/org/org3/teams/quiet", members.Find("a").AttrOr("href", ""))
		repos := doc.Find(`[data-testid="org-team-no-repos"]`)
		require.Equal(t, 1, repos.Length())
		assert.Contains(t, repos.Text(), "This team has no repositories yet.")
		assert.Equal(t, "/org/org3/teams/quiet/repositories", repos.Find("a").AttrOr("href", ""))
	})

	t.Run("delete asks to confirm", func(t *testing.T) {
		resp := owner.MakeRequest(t, NewRequest(t, "GET", "/org/org3/teams/quiet/edit"), http.StatusOK)
		doc := NewHTMLParser(t, resp.Body)
		btn := doc.Find(`button.delete-button[data-modal-id="delete-team"]`)
		require.Equal(t, 1, btn.Length())
		assert.Equal(t, "/org/org3/teams/quiet/delete", btn.AttrOr("data-url", ""))
		assert.Equal(t, 1, doc.Find(`#delete-team.g-modal-confirm`).Length())
	})
}
