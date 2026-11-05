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

// TestAscProfileStates checks the states of a profile (board Profile): the owner's edit and view-as-visitor
// buttons, the visitor view, the loading skeleton, the private banner and the empty row.
func TestAscProfileStates(t *testing.T) {
	defer tests.PrepareTestEnv(t)()

	t.Run("Owner", func(t *testing.T) {
		session := loginUser(t, "user2")
		page := NewHTMLParser(t, session.MakeRequest(t, NewRequest(t, "GET", "/user2"), http.StatusOK).Body)
		page.AssertElement(t, "[data-asc-pf-edit]", true)
		toggle := page.Find("[data-asc-pf-visitor]")
		pressed, _ := toggle.Attr("aria-pressed")
		assert.Equal(t, "false", pressed)
		href, _ := toggle.Attr("href")
		assert.Contains(t, href, "visitor=1")
		assert.Equal(t, 1, page.Find("[data-asc-list-main] > [data-asc-list-loading][hidden]").Length())
		page.AssertElement(t, "[data-asc-pf-private]", false)
	})

	t.Run("VisitorView", func(t *testing.T) {
		session := loginUser(t, "user2")
		page := NewHTMLParser(t, session.MakeRequest(t, NewRequest(t, "GET", "/user2?visitor=1"), http.StatusOK).Body)
		toggle := page.Find("[data-asc-pf-visitor]")
		pressed, _ := toggle.Attr("aria-pressed")
		assert.Equal(t, "true", pressed)
		href, _ := toggle.Attr("href")
		assert.NotContains(t, href, "visitor=1")
		page.AssertElement(t, "#action-block", false)
		page.AssertElement(t, "a[href='/user/settings/pins']", false)
	})

	t.Run("OtherUser", func(t *testing.T) {
		session := loginUser(t, "user5")
		page := NewHTMLParser(t, session.MakeRequest(t, NewRequest(t, "GET", "/user2"), http.StatusOK).Body)
		page.AssertElement(t, "[data-asc-pf-edit]", false)
		page.AssertElement(t, "[data-asc-pf-visitor]", false)
	})

	t.Run("Private", func(t *testing.T) {
		// user31 is private and follows user33, so user33 sees the limited profile
		session := loginUser(t, "user33")
		page := NewHTMLParser(t, session.MakeRequest(t, NewRequest(t, "GET", "/user31"), http.StatusOK).Body)
		banner := page.Find("[data-asc-pf-private]")
		assert.Equal(t, 1, banner.Length())
		assert.Contains(t, strings.TrimSpace(banner.Text()), "user31 keeps their profile private")
		page.AssertElement(t, ".wk-pf-stats", false)

		// any tab shows only the repositories
		page = NewHTMLParser(t, session.MakeRequest(t, NewRequest(t, "GET", "/user31?tab=activity"), http.StatusOK).Body)
		page.AssertElement(t, "[data-asc-pf-private]", true)
		page.AssertElement(t, "#wk-heat-title", false)

		// the person sees the whole profile
		page = NewHTMLParser(t, loginUser(t, "user31").MakeRequest(t, NewRequest(t, "GET", "/user31"), http.StatusOK).Body)
		page.AssertElement(t, "[data-asc-pf-private]", false)
	})

	t.Run("Empty", func(t *testing.T) {
		page := NewHTMLParser(t, emptyTestSession(t).MakeRequest(t, NewRequest(t, "GET", "/user5"), http.StatusOK).Body)
		empty := page.Find("[data-asc-pf-empty]")
		assert.Equal(t, 1, empty.Length())
		assert.Contains(t, empty.Text(), "No pins or contributions yet")
		page.AssertElement(t, "[data-asc-pf-empty] a[href='/user/settings/pins']", false)

		page = NewHTMLParser(t, loginUser(t, "user5").MakeRequest(t, NewRequest(t, "GET", "/user5"), http.StatusOK).Body)
		page.AssertElement(t, "[data-asc-pf-empty] a[href='/user/settings/pins']", true)
	})
}
