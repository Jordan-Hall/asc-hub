// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package integration

import (
	"net/http"
	"strings"
	"testing"

	"forgejo.org/tests"

	"github.com/stretchr/testify/assert"
)

// TestAscDashboardStates checks the states of the dashboard (board Dashboard): the loading skeleton of the main
// column, and the Today card with its offline state, which loads alone from /-/asc/dash/today.
func TestAscDashboardStates(t *testing.T) {
	defer tests.PrepareTestEnv(t)()
	session := loginUser(t, "user2")

	page := NewHTMLParser(t, session.MakeRequest(t, NewRequest(t, "GET", "/"), http.StatusOK).Body)
	skeleton := page.Find(".wk-dash-main [data-asc-list-main] > [data-asc-list-loading][hidden]")
	assert.Equal(t, 1, skeleton.Length())
	assert.Equal(t, 3, skeleton.Find(".wk-sk-row").Length())
	page.AssertElement(t, "[data-asc-list-main] > .wk-dash-work > section [id='wk-needs-title']", true)

	today := page.Find("[data-asc-dash-today]")
	assert.Equal(t, 1, today.Length())
	url, _ := today.Attr("data-asc-dash-today")
	assert.Equal(t, "/-/asc/dash/today", url)
	offline := today.Find("[data-asc-dash-offline][hidden]")
	assert.Equal(t, 1, offline.Length())
	assert.Equal(t, "Offline. Activity could not load. Your stacks and inbox are fine.", strings.TrimSpace(offline.Find("[role=alert]").Text()))
	assert.Equal(t, "Try again", strings.TrimSpace(offline.Find("button[data-asc-dash-retry]").Text()))
	tmpl, _ := offline.Find("[data-asc-dash-countdown]").Attr("data-template")
	assert.Equal(t, "Retrying in %d s", tmpl)

	// the card alone, never cached, with the same heading and offline state
	resp := session.MakeRequest(t, NewRequest(t, "GET", "/-/asc/dash/today"), http.StatusOK)
	assert.Equal(t, "private, no-store", resp.Header().Get("Cache-Control"))
	card := NewHTMLParser(t, resp.Body)
	assert.Equal(t, 1, card.Find("section[data-asc-dash-today]").Length())
	assert.Equal(t, "Today", strings.TrimSpace(card.Find("#wk-today-title").Text()))
	card.AssertElement(t, "[data-asc-dash-offline][hidden]", true)
	card.AssertElement(t, "[data-asc-list-main]", false)

	// signed out: the sign-in page, not a card
	resp = emptyTestSession(t).MakeRequest(t, NewRequest(t, "GET", "/-/asc/dash/today"), http.StatusSeeOther)
	assert.Contains(t, resp.Header().Get("Location"), "/user/login")
}
