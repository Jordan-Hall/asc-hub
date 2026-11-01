// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package integration

import (
	"net/http"
	"testing"

	"forgejo.org/tests"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The Sessions tab of a person in the administration lists their browsers; the admin signs out one, or all with a reason.
func TestAdminSignsAPersonOutOfTheirBrowsers(t *testing.T) {
	defer tests.PrintCurrentTest(t)()
	defer tests.PrepareTestEnv(t)()

	laptop := loginUser(t, "user2")
	laptop.MakeRequest(t, NewRequest(t, "GET", "/user/settings"), http.StatusOK)
	phone := loginUser(t, "user2")
	phone.MakeRequest(t, NewRequest(t, "GET", "/user/settings"), http.StatusOK)

	admin := loginUser(t, "user1")
	tab := "/admin/users/2?tab=sessions"
	doc := NewHTMLParser(t, admin.MakeRequest(t, NewRequest(t, "GET", tab), http.StatusOK).Body)
	rows := doc.Find(`[data-testid="admin-user-browser"]`)
	require.Equal(t, 2, rows.Length())
	id, ok := rows.First().Find(`input[name="id"]`).Attr("value")
	require.True(t, ok)

	// one browser
	admin.MakeRequest(t, NewRequestWithValues(t, "POST", "/admin/users/2/sessions/browser/revoke", map[string]string{"id": id}), http.StatusSeeOther)
	doc = NewHTMLParser(t, admin.MakeRequest(t, NewRequest(t, "GET", tab), http.StatusOK).Body)
	assert.Equal(t, 1, doc.Find(`[data-testid="admin-user-browser"]`).Length())

	// everywhere needs a reason
	admin.MakeRequest(t, NewRequestWithValues(t, "POST", "/admin/users/2/sessions/browser/revoke_all", map[string]string{}), http.StatusSeeOther)
	doc = NewHTMLParser(t, admin.MakeRequest(t, NewRequest(t, "GET", tab), http.StatusOK).Body)
	assert.Equal(t, 1, doc.Find(`[data-testid="admin-user-browser"]`).Length())
	admin.MakeRequest(t, NewRequestWithValues(t, "POST", "/admin/users/2/sessions/browser/revoke_all", map[string]string{"reason": "Lost laptop"}), http.StatusSeeOther)
	laptop.MakeRequest(t, NewRequest(t, "GET", "/user/settings"), http.StatusSeeOther)
	phone.MakeRequest(t, NewRequest(t, "GET", "/user/settings"), http.StatusSeeOther)
	doc = NewHTMLParser(t, admin.MakeRequest(t, NewRequest(t, "GET", tab), http.StatusOK).Body)
	assert.Equal(t, 0, doc.Find(`[data-testid="admin-user-browser"]`).Length())
	admin.MakeRequest(t, NewRequest(t, "GET", "/user/settings"), http.StatusOK)

	// not for people who are no admin
	other := loginUser(t, "user4")
	other.MakeRequest(t, NewRequestWithValues(t, "POST", "/admin/users/2/sessions/browser/revoke_all", map[string]string{"reason": "x"}), http.StatusForbidden)
}
