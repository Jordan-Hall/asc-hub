// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package integration

import (
	"net/http"
	"strings"
	"testing"

	"forgejo.org/models/db"
	user_model "forgejo.org/models/user"
	"forgejo.org/tests"

	"github.com/PuerkitoBio/goquery"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Board notes of the people list: chips narrow the list and keep the filter in the address, the row opens the person and
// Edit the form, suspended people stay visible with a red chip, the skeleton waits for the next page, and no matches.
func TestAscAdminUsersBoardNotes(t *testing.T) {
	defer tests.PrepareTestEnv(t)()
	_, err := db.GetEngine(t.Context()).ID(5).Cols("prohibit_login").Update(&user_model.User{ProhibitLogin: true})
	require.NoError(t, err)
	session := loginUser(t, "user1")
	get := func(path string) *HTMLDoc {
		return NewHTMLParser(t, session.MakeRequest(t, NewRequest(t, "GET", path), http.StatusOK).Body)
	}
	rowOf := func(doc *HTMLDoc, id string) *goquery.Selection {
		return doc.Find(`.adm-rowlink tbody tr`).FilterFunction(func(_ int, tr *goquery.Selection) bool {
			return tr.Find(`a.adm-who-name[href="/admin/users/`+id+`"]`).Length() == 1
		})
	}

	// default: every person, the row opens the person, Edit opens the form, the skeleton is there but hidden
	doc := get("/admin/users")
	row := rowOf(doc, "2")
	require.Equal(t, 1, row.Length())
	assert.Equal(t, "/admin/users/2/edit", row.Find("a.ds-btn").AttrOr("href", ""))
	loading := doc.Find(`[data-asc-list-main] [data-asc-list-loading]`)
	require.Equal(t, 1, loading.Length())
	_, hidden := loading.Attr("hidden")
	assert.True(t, hidden)
	_, filter := doc.Find("#user-list-search-form").Attr("data-asc-list-filter")
	assert.True(t, filter, "the search shows the skeleton while it loads")

	// suspended people stay in the full list, with a red chip
	suspended := rowOf(doc, "5")
	require.Equal(t, 1, suspended.Length())
	assert.Contains(t, suspended.Find(".ds-chip-err").Text(), "Suspended")

	// a chip keeps the filter in the address and narrows the list to it
	var href string
	doc.Find(".adm-filter nav a").Each(func(_ int, a *goquery.Selection) {
		if strings.Contains(a.Text(), "Suspended") {
			href = a.AttrOr("href", "")
		}
	})
	require.Contains(t, href, "status_filter%5Bis_prohibit_login%5D=1")
	doc = get(href)
	ids := []string{}
	doc.Find(".adm-rowlink tbody a.adm-who-name").Each(func(_ int, a *goquery.Selection) { ids = append(ids, a.AttrOr("href", "")) })
	assert.Contains(t, ids, "/admin/users/5")
	assert.NotContains(t, ids, "/admin/users/2")
	assert.Equal(t, len(ids), doc.Find(".adm-rowlink tbody .ds-chip-err").Length(), "every row is a suspended person")
	assert.Positive(t, doc.Find(`.adm-filter nav [aria-current="true"]`).FilterFunction(func(_ int, s *goquery.Selection) bool {
		return strings.Contains(s.Text(), "Suspended")
	}).Length(), "the chip in use is marked")
	val, _ := doc.Find(`#user-list-search-form input[name="status_filter[is_prohibit_login]"]`).Attr("value")
	assert.Equal(t, "1", val, "a search keeps the chip")

	// no matches
	doc = get("/admin/users?q=zz-nobody")
	assert.Equal(t, 0, doc.Find(".adm-rowlink").Length())
	assert.Contains(t, doc.Find(".ds-empty").Text(), "Clear filters")
}
