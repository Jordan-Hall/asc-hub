// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package integration

import (
	"net/http"
	"testing"

	"forgejo.org/tests"

	"github.com/stretchr/testify/assert"
)

// Local passwords: admins only. Once it is on, a person who is no admin cannot sign in with a password kept here.
func TestLocalPasswordsAdminsOnly(t *testing.T) {
	defer tests.PrintCurrentTest(t)()
	defer tests.PrepareTestEnv(t)()

	admin := loginUser(t, "user1")
	doc := NewHTMLParser(t, admin.MakeRequest(t, NewRequest(t, "GET", "/admin/auths"), http.StatusOK).Body)
	assert.Equal(t, 0, doc.Find(`[data-testid="auth-local-form"] input[name="admins_only"][checked]`).Length())

	before := loginUser(t, "user2")
	admin.MakeRequest(t, NewRequestWithValues(t, "POST", "/admin/auths/local", map[string]string{"admins_only": "true"}), http.StatusSeeOther)
	// a session begun before is signed out on its next request
	before.MakeRequest(t, NewRequest(t, "GET", "/user/settings"), http.StatusSeeOther)
	doc = NewHTMLParser(t, admin.MakeRequest(t, NewRequest(t, "GET", "/admin/auths"), http.StatusOK).Body)
	assert.Equal(t, 1, doc.Find(`[data-testid="auth-local-form"] input[name="admins_only"][checked]`).Length())

	// a person who is no admin is refused; the admin still signs in
	req := NewRequestWithValues(t, "POST", "/user/login", map[string]string{"user_name": "user2", "password": userPassword})
	MakeRequest(t, req, http.StatusOK) // the page that says signing in is not allowed, no redirect
	loginUser(t, "user1")

	// the API with a password is refused too
	MakeRequest(t, NewRequest(t, "GET", "/api/v1/user").AddBasicAuth("user2"), http.StatusUnauthorized)

	// turned off, everyone signs in again
	admin.MakeRequest(t, NewRequestWithValues(t, "POST", "/admin/auths/local", map[string]string{}), http.StatusSeeOther)
	loginUser(t, "user2")

	// only an admin can change it
	user := loginUser(t, "user2")
	user.MakeRequest(t, NewRequestWithValues(t, "POST", "/admin/auths/local", map[string]string{"admins_only": "true"}), http.StatusForbidden)
}
