// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package integration

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// The History shows the Landing rate card for the viewed branch when something landed in the last twelve weeks:
// this week's count, the change on last week and a bar per week. A branch whose changes are older has no card.
func TestAscHistoryLandingRate(t *testing.T) {
	onApplicationRun(t, func(t *testing.T, _ *url.URL) {
		session := loginUser(t, "user1")
		testRepoFork(t, session, "user2", "repo1", "user1", "repo1")

		resp := session.MakeRequest(t, NewRequest(t, "GET", "/user1/repo1/commits/branch/master"), http.StatusOK)
		assert.Zero(t, NewHTMLParser(t, resp.Body).Find(`[data-testid="history-landing"]`).Length(), "the fixture changes are years old")

		testEditFileToNewBranch(t, session, "user1", "repo1", "master", "fresh", "README.md", "Fresh\n")
		resp = session.MakeRequest(t, NewRequest(t, "GET", "/user1/repo1/commits/branch/fresh"), http.StatusOK)
		card := NewHTMLParser(t, resp.Body).Find(`[data-testid="history-landing"]`)
		assert.Equal(t, 1, card.Length())
		assert.Contains(t, card.Find(".rp-rail-title").Text(), "Landing rate")
		assert.Contains(t, card.Text(), "12 weeks")
		assert.Equal(t, "1", strings.TrimSpace(card.Find(`[data-testid="history-landing-week"]`).Text()))
		assert.Contains(t, card.Find(".rp-landing-delta.is-up").Text(), "+1 on last week")
		assert.Equal(t, 12, card.Find("rect").Length())
		assert.Equal(t, 1, card.Find("rect.is-now").Length())
		label, _ := card.Find("svg").Attr("aria-label")
		assert.Equal(t, "1 changes landed in the last 12 weeks, 1 of them this week, per week", label)
	})
}
