// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package integration

import (
	"net/http"
	"testing"

	"forgejo.org/modules/setting"
	"forgejo.org/modules/test"
	"forgejo.org/tests"

	"github.com/stretchr/testify/assert"
)

// TestAscOrgRepoList checks the repository list of an organization (boards OrgProfile and Mobile-OrgProfile): a hidden
// skeleton card the search, the sort and the pager show while the page loads, and a row menu that holds the row
// actions on a phone. Other repository lists keep their actions in the row.
func TestAscOrgRepoList(t *testing.T) {
	defer tests.PrepareTestEnv(t)()
	defer test.MockVariableValue(&setting.Asc.Enabled, true)()
	session := loginUser(t, "user2")

	page := NewHTMLParser(t, session.MakeRequest(t, NewRequest(t, "GET", "/org3"), http.StatusOK).Body)
	page.AssertElement(t, "#repo-search-form[data-asc-list-filter]", true)
	page.AssertElement(t, ".wk-org-list[data-asc-list-main] > [data-asc-list-loading][hidden][role=status][aria-busy=true]", true)
	assert.Equal(t, 4, page.Find("[data-asc-list-loading] .wk-sk-row").Length())
	page.AssertElement(t, ".wk-org-list[data-asc-list-main] > .ds-repolist", true)

	rows := page.Find(".ds-repo-row")
	assert.Positive(t, rows.Length())
	assert.Equal(t, rows.Length(), page.Find(".ds-repo-row details[data-asc-row-menu]").Length(), "every row has a menu")
	assert.Equal(t, rows.Length(), page.Find(".ds-repo-row .ds-row-acts.has-row-menu").Length())
	clone, _ := page.Find(".ds-repo-row details[data-asc-row-menu] .wk-menu-item[data-clipboard-text]").First().Attr("data-clipboard-text")
	assert.Contains(t, clone, "asc clone ")
	label, _ := page.Find(".ds-repo-row details[data-asc-row-menu] > summary").First().Attr("aria-label")
	assert.Contains(t, label, "org3/")

	explore := NewHTMLParser(t, session.MakeRequest(t, NewRequest(t, "GET", "/explore/repos"), http.StatusOK).Body)
	explore.AssertElement(t, "details[data-asc-row-menu]", false)
	explore.AssertElement(t, "[data-asc-list-main]", false)
	explore.AssertElement(t, ".ds-row-acts.has-row-menu", false)
}
