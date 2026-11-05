// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package integration

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"forgejo.org/modules/setting"
	"forgejo.org/modules/test"
	auth_method "forgejo.org/services/auth/method"
	"forgejo.org/tests"

	"github.com/stretchr/testify/assert"
)

func ascSignIn(t *testing.T, name, password string, status int) *httptest.ResponseRecorder {
	t.Helper()
	req := NewRequestWithValues(t, "POST", "/user/login", map[string]string{"user_name": name, "password": password})
	return emptyTestSession(t).MakeRequest(t, req, status)
}

func ascSignInFlash(t *testing.T, resp *httptest.ResponseRecorder) string {
	t.Helper()
	return strings.TrimSpace(NewHTMLParser(t, resp.Body).Find("#flash-message p").Text())
}

// TestAscLoginWait checks the wait after wrong passwords (board Onboarding): the message of a wrong password names
// the rule, the try after the last allowed one waits with 429 and Retry-After, even with the right password, every
// spelling of the account counts the same, another account from the same address does not wait, a right password
// before the limit clears the count, and basic authentication shares the count.
func TestAscLoginWait(t *testing.T) {
	defer tests.PrepareTestEnv(t)()
	defer test.MockVariableValue(&setting.LoginMaxAttempts, 3)()
	defer test.MockVariableValue(&setting.LoginWait, 2*time.Minute)()
	auth_method.ResetLoginWaits()
	defer auth_method.ResetLoginWaits()

	t.Run("Wait", func(t *testing.T) {
		wrong := "Could not sign in. The username or password is wrong. After 3 attempts we wait 2 minutes."
		assert.Equal(t, wrong, ascSignInFlash(t, ascSignIn(t, "user5", "wrongPassword", http.StatusOK)))
		assert.Equal(t, wrong, ascSignInFlash(t, ascSignIn(t, "USER5", "wrongPassword", http.StatusOK)))
		// the email of the same person is the same account: the third wrong try starts the wait
		resp := ascSignIn(t, "user5@example.com", "wrongPassword", http.StatusTooManyRequests)
		assert.Equal(t, "120", resp.Header().Get("Retry-After"))
		assert.Equal(t, "Too many attempts to sign in. Try again in 2 minutes.", ascSignInFlash(t, resp))
		ascSignIn(t, "user5", userPassword, http.StatusTooManyRequests)

		// basic authentication of the same account waits too
		req := NewRequest(t, "GET", "/api/v1/user").AddBasicAuth("user5")
		MakeRequest(t, req, http.StatusUnauthorized)

		// another account from the same address may still sign in
		ascSignIn(t, "user10", userPassword, http.StatusSeeOther)
	})

	t.Run("RightPasswordClears", func(t *testing.T) {
		for range 3 {
			ascSignIn(t, "user11", "wrongPassword", http.StatusOK)
			ascSignIn(t, "user11", "wrongPassword", http.StatusOK)
			ascSignIn(t, "user11", userPassword, http.StatusSeeOther)
		}
	})

	t.Run("Off", func(t *testing.T) {
		defer test.MockVariableValue(&setting.LoginMaxAttempts, 0)()
		for range 4 {
			assert.Equal(t, "Username or password is incorrect.", ascSignInFlash(t, ascSignIn(t, "user12", "wrongPassword", http.StatusOK)))
		}
	})
}
