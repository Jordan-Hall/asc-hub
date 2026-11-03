// Copyright 2024 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package integration

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"forgejo.org/modules/setting"
	"forgejo.org/modules/test"
	"forgejo.org/modules/translation"
	"forgejo.org/tests"

	"github.com/stretchr/testify/assert"
)

func TestNewOrganizationForm(t *testing.T) {
	onApplicationRun(t, func(t *testing.T, giteaURL *url.URL) {
		session := loginUser(t, "user1")
		locale := translation.NewLocale("en-US")

		response := session.MakeRequest(t, NewRequest(t, "GET", "/org/create"), http.StatusOK)
		page := NewHTMLParser(t, response.Body)

		// Verify page title
		title := page.Find("title").Text()
		assert.Contains(t, title, locale.TrString("new_org.title"))

		// Verify page form
		_, exists := page.Find("form[action='/org/create']").Attr("method")
		assert.True(t, exists)

		// Verify page header
		header := strings.TrimSpace(page.Find("h1.wk-title").Text())
		assert.Equal(t, locale.TrString("new_org.title"), header)
	})
}

func TestNewOrganizationHandleCheck(t *testing.T) {
	defer tests.PrepareTestEnv(t)()
	defer test.MockVariableValue(&setting.Service.AllowDotsInUsernames, true)()
	session := loginUser(t, "user1")

	check := func(name string) (ok bool, message string) {
		var res struct {
			OK      bool   `json:"ok"`
			Message string `json:"message"`
		}
		DecodeJSON(t, session.MakeRequest(t, NewRequest(t, "GET", "/org/create/check?name="+url.QueryEscape(name)), http.StatusOK), &res)
		return res.OK, res.Message
	}

	ok, msg := check("nova-team")
	assert.True(t, ok)
	assert.Equal(t, "nova-team is free.", msg)
	ok, msg = check("  ")
	assert.False(t, ok)
	assert.Empty(t, msg, "nothing typed, nothing said")
	ok, msg = check("org3")
	assert.False(t, ok)
	assert.Equal(t, "The organization name is already taken.", msg)
	ok, msg = check("user2")
	assert.False(t, ok, "a person's name is taken too")
	assert.Equal(t, "The organization name is already taken.", msg)
	ok, msg = check("explore")
	assert.False(t, ok)
	assert.Equal(t, `The organization name "explore" is reserved.`, msg)
	ok, msg = check("nova.keys")
	assert.False(t, ok)
	assert.Contains(t, msg, `"*.keys" is not allowed`)
	ok, msg = check("-nova")
	assert.False(t, ok)
	assert.Contains(t, msg, "start and end with a letter or digit")
	ok, msg = check(strings.Repeat("a", 41))
	assert.False(t, ok)
	assert.Equal(t, "Use at most 40 characters.", msg)

	// someone who may not make organizations is told so, and the page needs a session
	resp := loginUser(t, "user5").MakeRequest(t, NewRequest(t, "GET", "/org/create/check?name=nova"), http.StatusForbidden)
	assert.Contains(t, resp.Body.String(), "not allowed")
	MakeRequest(t, NewRequest(t, "GET", "/org/create/check?name=nova"), http.StatusSeeOther)

	// the form carries the address of the check, the status line and the preview
	page := NewHTMLParser(t, session.MakeRequest(t, NewRequest(t, "GET", "/org/create"), http.StatusOK).Body)
	page.AssertElement(t, `#org-new-form[data-check-url="/org/create/check"]`, true)
	page.AssertElement(t, `#org_name[aria-describedby="org_name_status org_name_help"]`, true)
	page.AssertElement(t, `[data-testid="org-preview"] [data-preview-url]`, true)
	page.AssertElement(t, `[data-testid="org-full-name"]`, true)
	page.AssertElement(t, `[data-testid="org-owner"]`, true)
	page.AssertElement(t, `[data-testid="org-first-team"] input[name="create_everyone"][checked]`, true)

	// a new organization is greeted on its dashboard
	resp = session.MakeRequest(t, NewRequestWithValues(t, "POST", "/org/create", map[string]string{"org_name": "nova-team", "visibility": "0"}), http.StatusSeeOther)
	page = NewHTMLParser(t, session.MakeRequest(t, NewRequest(t, "GET", resp.Header().Get("Location")), http.StatusOK).Body)
	assert.Contains(t, page.Find(".flash-message").Text(), "nova-team is ready.")
}
