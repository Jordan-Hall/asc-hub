// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package integration

import (
	"net/http"
	"net/url"
	"testing"

	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"
	"forgejo.org/tests"

	"github.com/stretchr/testify/assert"
)

// TestAscPrefsFollowTheAccount checks that the colour mode and the density chosen in the header (board Shell) are
// saved with the account, so another browser of the person opens the same way, and that a guest keeps them in the
// browser only.
func TestAscPrefsFollowTheAccount(t *testing.T) {
	defer tests.PrepareTestEnv(t)()
	session := loginUser(t, "user2")
	post := func(status int, form url.Values) {
		t.Helper()
		session.MakeRequest(t, NewRequestWithURLValues(t, "POST", "/-/asc/prefs", form), status)
	}

	post(http.StatusNoContent, url.Values{"theme": {"dark"}, "density": {"compact"}})
	assert.Equal(t, "asc-dark", unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2}).Theme)

	// another browser of the same person
	other := loginUser(t, "user2")
	resp := other.MakeRequest(t, NewRequest(t, "GET", "/user/settings/appearance"), http.StatusOK)
	body := resp.Body.String()
	assert.Contains(t, body, `var accountDensity = "compact";`)
	assert.Contains(t, body, `var signed = true;`)
	page := NewHTMLParser(t, resp.Body)
	media, _ := page.Find("#asc-theme-dark").Attr("media")
	assert.Equal(t, "all", media)
	content, _ := page.Find(`meta[name="asc-prefs"]`).Attr("content")
	assert.Equal(t, "/-/asc/prefs", content)
	assert.Equal(t, 2, page.Find(`button[data-ds-density-set="compact"]`).Length(), "the header menu and the Appearance page")

	post(http.StatusBadRequest, url.Values{"theme": {"sepia"}})
	post(http.StatusBadRequest, url.Values{"density": {"huge"}})
	assert.Equal(t, "asc-dark", unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2}).Theme)

	guest := MakeRequest(t, NewRequest(t, "GET", "/explore/repos"), http.StatusOK)
	NewHTMLParser(t, guest.Body).AssertElement(t, `meta[name="asc-prefs"]`, false)
	MakeRequest(t, NewRequestWithURLValues(t, "POST", "/-/asc/prefs", url.Values{"theme": {"light"}}), http.StatusSeeOther)
}
