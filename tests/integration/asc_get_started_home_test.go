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

// TestAscGetStartedOnHome checks the checklist on the dashboard (board GetStarted): a person with no repository lands
// on Get started at /, with Hide, which brings the dashboard back until they sign in again; a person with
// repositories gets the dashboard.
func TestAscGetStartedOnHome(t *testing.T) {
	defer tests.PrepareTestEnv(t)()

	t.Run("NoRepository", func(t *testing.T) {
		session := loginUser(t, "user8")
		page := NewHTMLParser(t, session.MakeRequest(t, NewRequest(t, "GET", "/"), http.StatusOK).Body)
		page.AssertElement(t, ".site-started", true)
		assert.Equal(t, "Your hub is empty. Let’s fix that.", strings.TrimSpace(page.Find(".site-started h1").Text()))
		form := page.Find("form[action='/-/get-started/hide'][method='post']")
		assert.Equal(t, 1, form.Length())
		label, _ := form.Find("button").Attr("aria-label")
		assert.Equal(t, "Hide until you sign in again", label)

		req := NewRequestWithValues(t, "POST", "/-/get-started/hide", map[string]string{})
		resp := session.MakeRequest(t, req, http.StatusSeeOther)
		assert.Equal(t, "/", resp.Header().Get("Location"))
		page = NewHTMLParser(t, session.MakeRequest(t, NewRequest(t, "GET", "/"), http.StatusOK).Body)
		page.AssertElement(t, ".site-started", false)

		// a new sign-in brings it back while there is still no repository
		page = NewHTMLParser(t, loginUser(t, "user8").MakeRequest(t, NewRequest(t, "GET", "/"), http.StatusOK).Body)
		page.AssertElement(t, ".site-started", true)
	})

	t.Run("WithRepositories", func(t *testing.T) {
		page := NewHTMLParser(t, loginUser(t, "user2").MakeRequest(t, NewRequest(t, "GET", "/"), http.StatusOK).Body)
		page.AssertElement(t, ".site-started", false)
	})

	t.Run("HideNeedsASignIn", func(t *testing.T) {
		resp := emptyTestSession(t).MakeRequest(t, NewRequest(t, "POST", "/-/get-started/hide"), http.StatusSeeOther)
		assert.Contains(t, resp.Header().Get("Location"), "/user/login")
	})
}
