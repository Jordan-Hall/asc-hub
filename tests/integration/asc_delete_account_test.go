// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package integration

import (
	"net/http"
	"testing"
	"time"

	auth_model "forgejo.org/models/auth"
	"forgejo.org/models/db"
	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"
	"forgejo.org/tests"

	"github.com/pquerna/otp/totp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// With two-factor sign-in on, deleting the account asks a code as well: no code or a wrong one keeps the account, a code
// of the app deletes it.
func TestUserDeleteAccountAsksTwoFactorCode(t *testing.T) {
	defer tests.PrepareTestEnv(t)()

	session := loginUser(t, "user8")
	doc := NewHTMLParser(t, session.MakeRequest(t, NewRequest(t, "GET", "/user/settings/account"), http.StatusOK).Body)
	doc.AssertElement(t, `[data-testid="delete-passcode"]`, false)

	key, err := totp.Generate(totp.GenerateOpts{SecretSize: 40, Issuer: "asc-test", AccountName: "user8"})
	require.NoError(t, err)
	require.NoError(t, auth_model.NewTwoFactor(db.DefaultContext, &auth_model.TwoFactor{UID: 8}, key.Secret()))

	doc = NewHTMLParser(t, session.MakeRequest(t, NewRequest(t, "GET", "/user/settings/account"), http.StatusOK).Body)
	doc.AssertElement(t, `[data-testid="delete-passcode"] input[name="passcode"][autocomplete="one-time-code"]`, true)

	remove := func(passcode string, status int) *HTMLDoc {
		req := NewRequestWithValues(t, "POST", "/user/settings/account/delete", map[string]string{
			"username_confirm": "user8",
			"password":         userPassword,
			"passcode":         passcode,
		})
		return NewHTMLParser(t, session.MakeRequest(t, req, status).Body)
	}
	for _, passcode := range []string{"", "not-a-code"} {
		doc = remove(passcode, http.StatusOK)
		assert.Contains(t, doc.Find(".flash-error").Text(), "The two-factor code is not right")
		doc.AssertElement(t, `#passcode-confirmation[aria-invalid="true"]`, true)
		unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 8})
	}

	passcode, err := totp.GenerateCode(key.Secret(), time.Now())
	require.NoError(t, err)
	remove(passcode, http.StatusSeeOther)
	assertUserDeleted(t, 8, false)
}

// A recovery code is accepted in place of a code of the app.
func TestUserDeleteAccountWithRecoveryCode(t *testing.T) {
	defer tests.PrepareTestEnv(t)()

	session := loginUser(t, "user8")
	key, err := totp.Generate(totp.GenerateOpts{SecretSize: 40, Issuer: "asc-test", AccountName: "user8"})
	require.NoError(t, err)
	tfa := &auth_model.TwoFactor{UID: 8}
	recovery := tfa.GenerateScratchToken()
	require.NoError(t, auth_model.NewTwoFactor(db.DefaultContext, tfa, key.Secret()))

	req := NewRequestWithValues(t, "POST", "/user/settings/account/delete", map[string]string{
		"username_confirm": "user8",
		"password":         userPassword,
		"passcode":         recovery,
	})
	session.MakeRequest(t, req, http.StatusSeeOther)
	assertUserDeleted(t, 8, false)
}
