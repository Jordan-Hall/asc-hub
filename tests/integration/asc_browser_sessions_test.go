// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package integration

import (
	"net/http"
	"strings"
	"testing"

	"forgejo.org/tests"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Signed-in browsers: each one is listed on the security page, and any other one can be signed out from here.
func TestBrowserSessionsSignOutAnotherBrowser(t *testing.T) {
	defer tests.PrintCurrentTest(t)()
	defer tests.PrepareTestEnv(t)()

	laptop := loginUser(t, "user2")
	phone := loginUser(t, "user2")
	phone.MakeRequest(t, NewRequest(t, "GET", "/user/settings"), http.StatusOK)

	doc := NewHTMLParser(t, laptop.MakeRequest(t, NewRequest(t, "GET", "/user/settings/security"), http.StatusOK).Body)
	rows := doc.Find(`[data-testid="browser-session"]`)
	require.Equal(t, 2, rows.Length())
	assert.True(t, rows.First().Is("[data-current]"), "this browser comes first")
	assert.Equal(t, 0, rows.First().Find("form").Length(), "this browser is signed out from the account menu, not here")
	id, ok := rows.Eq(1).Find(`input[name="id"]`).Attr("value")
	require.True(t, ok)

	laptop.MakeRequest(t, NewRequestWithValues(t, "POST", "/user/settings/security/sessions/revoke", map[string]string{"id": id}), http.StatusSeeOther)

	// the phone is signed out on its next request
	resp := phone.MakeRequest(t, NewRequest(t, "GET", "/user/settings"), http.StatusSeeOther)
	assert.True(t, strings.HasPrefix(resp.Header().Get("Location"), "/user/login"))
	laptop.MakeRequest(t, NewRequest(t, "GET", "/user/settings"), http.StatusOK)

	// sign out everywhere else
	tablet := loginUser(t, "user2")
	tablet.MakeRequest(t, NewRequest(t, "GET", "/user/settings"), http.StatusOK)
	laptop.MakeRequest(t, NewRequestWithValues(t, "POST", "/user/settings/security/sessions/revoke_others", nil), http.StatusSeeOther)
	tablet.MakeRequest(t, NewRequest(t, "GET", "/user/settings"), http.StatusSeeOther)
	doc = NewHTMLParser(t, laptop.MakeRequest(t, NewRequest(t, "GET", "/user/settings/security"), http.StatusOK).Body)
	assert.Equal(t, 1, doc.Find(`[data-testid="browser-session"]`).Length())
}
