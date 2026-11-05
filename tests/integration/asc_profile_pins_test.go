// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package integration

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"forgejo.org/tests"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAscProfilePins checks the pinned repositories (boards Profile and Dashboard): the owner reorders them by drag
// or in the Customize dialog, the cards count stacks and changes, and the dashboard lists the pins first with counts
// that load again while the page is open.
func TestAscProfilePins(t *testing.T) {
	defer tests.PrepareTestEnv(t)()
	session := loginUser(t, "user2")
	save := func(status int, ids ...string) {
		t.Helper()
		form := url.Values{"repo": ids, "inline": {"1"}}
		session.MakeRequest(t, NewRequestWithURLValues(t, "POST", "/user/settings/pins", form), status)
	}

	t.Run("Owner", func(t *testing.T) {
		save(http.StatusNoContent, "16", "1")
		page := NewHTMLParser(t, session.MakeRequest(t, NewRequest(t, "GET", "/user2"), http.StatusOK).Body)
		cards := page.Find("ul[data-asc-pins-cards] > li.wk-pin[draggable=true]")
		require.Equal(t, 2, cards.Length())
		first, _ := cards.First().Attr("data-id")
		assert.Equal(t, "16", first)
		assert.Equal(t, "user2/repo16", strings.TrimSpace(cards.First().Find(".wk-pin-name").Text()))
		assert.Regexp(t, `^\d+ stacks? · \d+ changes?$`, strings.TrimSpace(cards.Eq(1).Find("p.ds-hint").Last().Text()))

		// Customize opens the dialog; the link stays the way without script
		open := page.Find("a[data-asc-pins-open]")
		href, _ := open.Attr("href")
		assert.Equal(t, "/user/settings/pins", href)
		dialog := page.Find("dialog[data-asc-pins-dialog]")
		require.Equal(t, 1, dialog.Length())
		assert.Equal(t, "Pinned repositories", strings.TrimSpace(dialog.Find("header").Text()))
		rows := dialog.Find("form[data-asc-pins-inline] ol[data-asc-pins] > li.wk-pin-row")
		assert.Positive(t, rows.Length())
		id0, _ := rows.Eq(0).Attr("data-id")
		id1, _ := rows.Eq(1).Attr("data-id")
		assert.Equal(t, []string{"16", "1"}, []string{id0, id1}, "the pins lead in their order")
		_, checked := rows.Eq(0).Find("input[name=repo]").Attr("checked")
		assert.True(t, checked)
		up, _ := rows.Eq(0).Find("button[data-asc-pin-up]").Attr("aria-label")
		assert.Equal(t, "Move user2/repo16 up", up)
		page.AssertElement(t, "[data-asc-pins-status][aria-live=polite]", true)
	})

	t.Run("Refused", func(t *testing.T) {
		save(http.StatusBadRequest, "1", "2", "15", "16", "31", "33", "36")
		save(http.StatusBadRequest, "99999")
	})

	t.Run("OthersCannotReorder", func(t *testing.T) {
		for _, page := range []*HTMLDoc{
			NewHTMLParser(t, session.MakeRequest(t, NewRequest(t, "GET", "/user2?visitor=1"), http.StatusOK).Body),
			NewHTMLParser(t, loginUser(t, "user5").MakeRequest(t, NewRequest(t, "GET", "/user2"), http.StatusOK).Body),
		} {
			page.AssertElement(t, "ul.wk-pins", true)
			page.AssertElement(t, "[data-asc-pins-cards]", false)
			page.AssertElement(t, "[data-asc-pins-dialog]", false)
			page.AssertElement(t, "li.wk-pin[draggable]", false)
		}
	})

	t.Run("Dashboard", func(t *testing.T) {
		page := NewHTMLParser(t, session.MakeRequest(t, NewRequest(t, "GET", "/"), http.StatusOK).Body)
		nav := page.Find("[data-asc-dash-counts]")
		counted, _ := nav.Attr("data-asc-dash-counts")
		assert.Equal(t, "/-/asc/dash/counts", counted)
		links := nav.Find("a .wk-swatch")
		require.Positive(t, links.Length())
		assert.Equal(t, "user2/repo16", strings.TrimSpace(links.First().Parent().Text()), "the first pin leads the work switcher")
		assert.Positive(t, nav.Find("[data-asc-live-count=stacks]").Length())

		resp := session.MakeRequest(t, NewRequest(t, "GET", "/-/asc/dash/counts"), http.StatusOK)
		assert.Equal(t, "private, no-store", resp.Header().Get("Cache-Control"))
		var counts map[string]int
		require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &counts))
		for _, k := range []string{"needs", "stacks", "reviews", "agents"} {
			_, ok := counts[k]
			assert.True(t, ok, k)
		}
		MakeRequest(t, NewRequest(t, "GET", "/-/asc/dash/counts"), http.StatusSeeOther)
	})
}
