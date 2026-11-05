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

// TestAscDocsFeedback checks "Was this page helpful?" on a docs page (board Docs): the question and its note, an
// answer that comes back thanked, the counts only a site administrator sees, and the limit per address.
func TestAscDocsFeedback(t *testing.T) {
	defer tests.PrepareTestEnv(t)()
	anon := emptyTestSession(t)

	page := NewHTMLParser(t, anon.MakeRequest(t, NewRequest(t, "GET", "/-/docs/install"), http.StatusOK).Body)
	box := page.Find("section#site-feedback[data-asc-docs-feedback='install']")
	assert.Equal(t, 1, box.Length())
	assert.Equal(t, "Was this page helpful?", strings.TrimSpace(box.Find("h2").Text()))
	action, _ := box.Find("form").Attr("action")
	assert.Equal(t, "/-/docs/install/feedback", action)
	assert.Equal(t, "Yes", strings.TrimSpace(box.Find("button[name=helpful][value=yes]").Text()))
	assert.Equal(t, "No", strings.TrimSpace(box.Find("button[name=helpful][value=no]").Text()))
	assert.Equal(t, "Feedback goes to the docs team", strings.TrimSpace(box.Find("[role=status]").Text()))
	assert.Equal(t, 0, box.Find("button[disabled]").Length())
	assert.Equal(t, 0, box.Find("[data-asc-docs-feedback-count]").Length())

	answer := func(slug, value string, status int) *http.Response {
		req := NewRequestWithValues(t, "POST", "/-/docs/"+slug+"/feedback", map[string]string{"helpful": value})
		return anon.MakeRequest(t, req, status).Result()
	}
	resp := answer("install", "yes", http.StatusSeeOther)
	assert.Equal(t, "/-/docs/install?feedback=thanks#site-feedback", resp.Header.Get("Location"))
	answer("install", "no", http.StatusSeeOther)
	answer("install", "maybe", http.StatusBadRequest)
	answer("no-such-page", "yes", http.StatusNotFound)

	thanked := NewHTMLParser(t, anon.MakeRequest(t, NewRequest(t, "GET", "/-/docs/install?feedback=thanks"), http.StatusOK).Body)
	assert.Equal(t, 2, thanked.Find("#site-feedback button[disabled]").Length())
	assert.Equal(t, "Thanks. Your answer went to the docs team.", strings.TrimSpace(thanked.Find("#site-feedback [role=status]").Text()))

	// the counts: a site administrator only
	admin := NewHTMLParser(t, loginUser(t, "user1").MakeRequest(t, NewRequest(t, "GET", "/-/docs/install"), http.StatusOK).Body)
	assert.Equal(t, "Readers: 1 found it helpful, 1 did not.", strings.TrimSpace(admin.Find("[data-asc-docs-feedback-count]").Text()))
	user := NewHTMLParser(t, loginUser(t, "user2").MakeRequest(t, NewRequest(t, "GET", "/-/docs/install"), http.StatusOK).Body)
	assert.Equal(t, 0, user.Find("[data-asc-docs-feedback-count]").Length())

	// ten answers a minute from one address
	limited := false
	for range 10 {
		req := NewRequestWithValues(t, "POST", "/-/docs/install/feedback", map[string]string{"helpful": "yes"})
		if anon.MakeRequest(t, req, NoExpectedStatus).Code == http.StatusTooManyRequests {
			limited = true
			break
		}
	}
	assert.True(t, limited)
}
