// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package integration

import (
	"net/http"
	"strings"
	"testing"

	"forgejo.org/modules/test"
	"forgejo.org/routers/web/user/setting/security"
	"forgejo.org/tests"

	"github.com/stretchr/testify/assert"
)

// A change to how the account signs in asks for the password again once the last proof is old.
func TestReauthBeforeRemovingTheSecondFactor(t *testing.T) {
	defer tests.PrintCurrentTest(t)()
	defer tests.PrepareTestEnv(t)()

	session := loginUser(t, "user2")
	session.EnrollTOTP(t)

	defer test.MockVariableValue(&security.ReauthFor, 0)()
	resp := session.MakeRequest(t, NewRequestWithValues(t, "POST", "/user/settings/security/two_factor/disable", nil), http.StatusSeeOther)
	loc := resp.Header().Get("Location")
	assert.True(t, strings.HasPrefix(loc, "/user/settings/security/reauth?redirect_to="), loc)

	// the second factor is still there
	doc := NewHTMLParser(t, session.MakeRequest(t, NewRequest(t, "GET", "/user/settings/security"), http.StatusOK).Body)
	assert.Positive(t, doc.Find(`form[action$="/two_factor/disable"]`).Length())

	// a wrong password is refused, and the redirect stays on this hub
	session.MakeRequest(t, NewRequestWithValues(t, "POST", "/user/settings/security/reauth", map[string]string{"password": "wrong", "redirect_to": "https://evil.example/"}), http.StatusOK)
	resp = session.MakeRequest(t, NewRequestWithValues(t, "POST", "/user/settings/security/reauth", map[string]string{"password": userPassword, "redirect_to": "https://evil.example/"}), http.StatusSeeOther)
	assert.Equal(t, "/user/settings/security", resp.Header().Get("Location"))

	// a fresh proof lets the change through
	test.MockVariableValue(&security.ReauthFor, security.ReauthFor+3600e9)
	session.MakeRequest(t, NewRequestWithValues(t, "POST", "/user/settings/security/two_factor/disable", nil), http.StatusSeeOther)
	doc = NewHTMLParser(t, session.MakeRequest(t, NewRequest(t, "GET", "/user/settings/security"), http.StatusOK).Body)
	assert.Equal(t, 0, doc.Find(`form[action$="/two_factor/disable"]`).Length())
}
