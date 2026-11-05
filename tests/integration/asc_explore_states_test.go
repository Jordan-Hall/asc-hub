// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package integration

import (
	"net/http"
	"testing"

	"forgejo.org/tests"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAscExploreNoMatch(t *testing.T) {
	defer tests.PrepareTestEnv(t)()

	doc := NewHTMLParser(t, MakeRequest(t, NewRequest(t, "GET", "/explore/repos?q=ledgr"), http.StatusOK).Body)
	empty := doc.doc.Find("[data-asc-explore-nomatch]")
	require.Equal(t, 1, empty.Length())
	assert.Equal(t, "Nothing matches “ledgr”", empty.Find("h2").Text())
	assert.Contains(t, empty.Find("p").First().Text(), "1 filter is active.")
	assert.Equal(t, 1, doc.doc.Find(".ds-active-filters").Length(), "the chips show once, in the empty state")
	assert.Equal(t, 1, empty.Find(".ds-active-filters a.ds-chip-btn").Length())
	assert.Zero(t, empty.Find(".ds-active-filters .ds-link-quiet").Length(), "the state has its own Clear filters button")
	assert.Equal(t, "?sort=recentupdate", empty.Find(".ds-empty-acts a.ds-btn-primary").AttrOr("href", ""), "Clear filters keeps the sort")

	doc = NewHTMLParser(t, MakeRequest(t, NewRequest(t, "GET", "/explore/repos?q=ledgr&language=Go"), http.StatusOK).Body)
	empty = doc.doc.Find("[data-asc-explore-nomatch]")
	assert.Contains(t, empty.Find("p").First().Text(), "2 filters are active.")
	assert.Equal(t, 2, empty.Find("a.ds-chip-btn").Length())

	// with results the chips stay above the list
	doc = NewHTMLParser(t, MakeRequest(t, NewRequest(t, "GET", "/explore/repos?q=repo1"), http.StatusOK).Body)
	assert.Zero(t, doc.doc.Find("[data-asc-explore-nomatch]").Length())
	assert.Equal(t, 1, doc.doc.Find(".ds-active-filters .ds-link-quiet").Length())
}

func TestAscExploreUsersRows(t *testing.T) {
	defer tests.PrepareTestEnv(t)()

	// a public membership of a public organization is named in the row; a private one is not
	doc := NewHTMLParser(t, MakeRequest(t, NewRequest(t, "GET", "/explore/users?q=user2"), http.StatusOK).Body)
	row := doc.doc.Find(`.ds-user-row:has(a[href="/user2"])`)
	require.Equal(t, 1, row.Length())
	assert.Equal(t, 1, row.Find(`[data-asc-user-orgs] a[href="/org3"]`).Length())
	assert.Zero(t, row.Find(".ds-row-acts form").Length(), "a visitor is offered no Follow")
	doc = NewHTMLParser(t, MakeRequest(t, NewRequest(t, "GET", "/explore/users?q=user4"), http.StatusOK).Body)
	assert.Zero(t, doc.doc.Find(`.ds-user-row:has(a[href="/user4"]) [data-asc-user-orgs]`).Length())

	// Follow on the row follows and comes back to the list
	session := loginUser(t, "user1")
	doc = NewHTMLParser(t, session.MakeRequest(t, NewRequest(t, "GET", "/explore/users?q=user2"), http.StatusOK).Body)
	form := doc.doc.Find(`.ds-user-row:has(a[href="/user2"]) .ds-row-acts form`)
	require.Equal(t, 1, form.Length())
	assert.Equal(t, "/user2?action=follow", form.AttrOr("action", ""))
	assert.Equal(t, "false", form.Find("button").AttrOr("aria-pressed", ""))
	resp := session.MakeRequest(t, NewRequestWithValues(t, "POST", "/user2?action=follow", map[string]string{
		"redirect_to": "/explore/users?q=user2",
	}), http.StatusSeeOther)
	assert.Equal(t, "/explore/users?q=user2", resp.Header().Get("Location"))
	doc = NewHTMLParser(t, session.MakeRequest(t, NewRequest(t, "GET", "/explore/users?q=user2"), http.StatusOK).Body)
	form = doc.doc.Find(`.ds-user-row:has(a[href="/user2"]) .ds-row-acts form`)
	assert.Equal(t, "/user2?action=unfollow", form.AttrOr("action", ""))
	assert.Equal(t, "true", form.Find("button").AttrOr("aria-pressed", ""))

	// a redirect elsewhere is not followed
	resp = session.MakeRequest(t, NewRequestWithValues(t, "POST", "/user2?action=unfollow", map[string]string{
		"redirect_to": "https://example.org/",
	}), http.StatusSeeOther)
	assert.Equal(t, "/user2", resp.Header().Get("Location"))

	// no match keeps the chip and offers Clear filters
	doc = NewHTMLParser(t, MakeRequest(t, NewRequest(t, "GET", "/explore/users?q=zzznobody"), http.StatusOK).Body)
	empty := doc.doc.Find("[data-asc-explore-nomatch]")
	require.Equal(t, 1, empty.Length())
	assert.Equal(t, "Nothing matches “zzznobody”", empty.Find("h2").Text())
	assert.Equal(t, 1, empty.Find("a.ds-chip-btn").Length())
}
