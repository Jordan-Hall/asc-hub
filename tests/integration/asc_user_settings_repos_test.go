// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package integration

import (
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"forgejo.org/models/db"
	issues_model "forgejo.org/models/issues"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/test"
	"forgejo.org/services/ascbyo/ascbyotest"
	"forgejo.org/tests"

	"github.com/PuerkitoBio/goquery"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func spRepoRows(t *testing.T, session *TestSession) (*HTMLDoc, map[string]*goquery.Selection) {
	t.Helper()
	doc := NewHTMLParser(t, session.MakeRequest(t, NewRequest(t, "GET", "/user/settings/repos"), http.StatusOK).Body)
	rows := map[string]*goquery.Selection{}
	doc.Find(`[data-testid="sp-repo-row"]`).Each(func(_ int, row *goquery.Selection) {
		rows[strings.TrimSpace(row.Find(".ds-li-title a").Text())] = row
	})
	return doc, rows
}

func spText(s *goquery.Selection) string { return strings.Join(strings.Fields(s.Text()), " ") }

// Settings > Repositories lists the repositories the user owns with their open stacks, where their objects live and a
// Transfer action.
func TestUserSettingsReposOwned(t *testing.T) {
	defer tests.PrepareTestEnv(t)()
	f := ascbyotest.Start(t)
	f.Placements = map[string]*ascv1.RepoPlacement{
		"user2/repo1": {TargetId: "st_AAAAAAAAAAAAAAAAAAAA", TargetName: "garage-eu", TargetState: ascv1.StorageTargetState_STORAGE_TARGET_STATE_ENABLED},
	}
	session := loginUser(t, "user2")

	doc, rows := spRepoRows(t, session)
	assert.Equal(t, "Repositories you own", spText(doc.Find(`[data-testid="sp-repos-owned"] h2`)))
	assert.Zero(t, doc.Find(`[data-testid="sp-repos-unadopted"]`).Length(), "user2 may not adopt or delete")

	repo1 := rows["user2/repo1"]
	require.NotNil(t, repo1)
	assert.Equal(t, "R", spText(repo1.Find(".wk-repo-tile")))
	assert.Contains(t, spText(repo1.Find(".ds-li-title")), "Public")
	open, err := db.GetEngine(t.Context()).Where("repo_id = ? AND is_pull = ? AND is_closed = ?", 1, true, false).Count(new(issues_model.Issue))
	require.NoError(t, err)
	assert.Equal(t, strconv.FormatInt(open, 10)+" stacks", spText(repo1.Find(`[data-testid="sp-repo-stacks"]`)))
	assert.Equal(t, "garage-eu", spText(repo1.Find(`[data-testid="repo-storage"]`)))
	assert.Contains(t, spText(rows["user2/repo2"].Find(".ds-li-title")), "Private")
	assert.Equal(t, "0 stacks", spText(rows["user2/repo2"].Find(`[data-testid="sp-repo-stacks"]`)))
	assert.Equal(t, "Server default", spText(rows["user2/repo2"].Find(`[data-testid="repo-storage"]`)))

	form := repo1.Find("dialog form")
	assert.Equal(t, "/user2/repo1/settings", form.AttrOr("action", ""))
	assert.Equal(t, "transfer", form.Find(`input[name="action"]`).AttrOr("value", ""))
	assert.Equal(t, 1, form.Find(`input[name="new_owner_name"]`).Length())
	assert.Equal(t, 1, form.Find(`input[name="repo_name"]`).Length())

	// The dialog posts to the repository's own transfer; a started transfer shows as pending.
	req := NewRequestWithValues(t, "POST", "/user2/repo1/settings", map[string]string{
		"action": "transfer", "repo_name": "user2/repo1", "new_owner_name": "user4",
	})
	session.MakeRequest(t, req, http.StatusSeeOther)
	_, rows = spRepoRows(t, session)
	assert.Contains(t, spText(rows["user2/repo1"].Find(".ds-li-title")), "Transfer pending")
	assert.Zero(t, rows["user2/repo1"].Find(`[data-testid="sp-repo-transfer"]`).Length())
	assert.Equal(t, "/user2/repo1/settings", rows["user2/repo1"].Find(".wk-row-acts a").AttrOr("href", ""))
}

// The Unadopted card lists directories on disk that have no repository record, for a user who may adopt them.
func TestUserSettingsReposUnadopted(t *testing.T) {
	defer tests.PrepareTestEnv(t)()
	session := loginUser(t, "user1")

	doc := NewHTMLParser(t, session.MakeRequest(t, NewRequest(t, "GET", "/user/settings/repos"), http.StatusOK).Body)
	card := doc.Find(`[data-testid="sp-repos-unadopted"]`)
	require.Equal(t, 1, card.Length())
	assert.Contains(t, spText(card), "No unadopted repositories")

	dir := filepath.Join(setting.RepoRootPath, "user1", "orphan.git")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	defer os.RemoveAll(dir)
	doc = NewHTMLParser(t, session.MakeRequest(t, NewRequest(t, "GET", "/user/settings/repos"), http.StatusOK).Body)
	row := doc.Find(`[data-testid="sp-unadopted-row"]`)
	require.Equal(t, 1, row.Length())
	assert.Contains(t, spText(row), "user1/orphan")
	assert.Zero(t, doc.Find(`[data-testid="sp-unadopted-more"]`).Length())

	defer test.MockVariableValue(&setting.Repository.AllowAdoptionOfUnadoptedRepositories, true)()
	doc = NewHTMLParser(t, loginUser(t, "user4").MakeRequest(t, NewRequest(t, "GET", "/user/settings/repos"), http.StatusOK).Body)
	assert.Equal(t, 1, doc.Find(`[data-testid="sp-repos-unadopted"]`).Length(), "shown once adopting is allowed")
}
