// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package integration

import (
	"net/http"
	"testing"

	repo_model "forgejo.org/models/repo"
	unit_model "forgejo.org/models/unit"
	"forgejo.org/models/unittest"
	"forgejo.org/tests"

	"github.com/stretchr/testify/assert"
)

// featureRows is the data-feature of each row of the Features card, in order.
func featureRows(t *testing.T, session *TestSession, link string) []string {
	t.Helper()
	page := NewHTMLParser(t, session.MakeRequest(t, NewRequest(t, "GET", link+"/settings"), http.StatusOK).Body)
	var rows []string
	for _, n := range page.Find("[data-testid=repo-features] [data-feature]").Nodes {
		for _, a := range n.Attr {
			if a.Key == "data-feature" {
				rows = append(rows, a.Val)
			}
		}
	}
	return rows
}

// TestAscRepoFeaturesGit checks the Features card of a git repository (board RepoSettingsGeneral): releases and the
// wiki are its switches, and turning one off removes the unit.
func TestAscRepoFeaturesGit(t *testing.T) {
	defer tests.PrepareTestEnv(t)()
	session := loginUser(t, "user2")

	assert.Equal(t, []string{"releases", "wiki"}, featureRows(t, session, "/user2/repo1"))

	session.MakeRequest(t, NewRequestWithValues(t, "POST", "/user2/repo1/settings/asc/features", map[string]string{"releases": "on"}), http.StatusSeeOther)
	repo := unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{ID: 1})
	assert.True(t, repo.UnitEnabled(t.Context(), unit_model.TypeReleases))
	assert.False(t, repo.UnitEnabled(t.Context(), unit_model.TypeWiki))
	session.MakeRequest(t, NewRequest(t, "GET", "/user2/repo1/wiki"), http.StatusNotFound)

	session.MakeRequest(t, NewRequestWithValues(t, "POST", "/user2/repo1/settings/asc/features", map[string]string{"wiki": "on"}), http.StatusSeeOther)
	repo = unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{ID: 1})
	assert.False(t, repo.UnitEnabled(t.Context(), unit_model.TypeReleases))
	assert.True(t, repo.UnitEnabled(t.Context(), unit_model.TypeWiki))

	// only an administrator of the repository saves the card
	other := loginUser(t, "user4")
	other.MakeRequest(t, NewRequestWithValues(t, "POST", "/user2/repo1/settings/asc/features", map[string]string{}), http.StatusNotFound)
}
