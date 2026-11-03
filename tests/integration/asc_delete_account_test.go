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
	"forgejo.org/modules/test"
	"forgejo.org/routers/web/user/setting/security"
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

// The scratch code is used up even when the deletion does not go ahead (here the account still owns repositories).
func TestUserDeleteAccountUsesUpTheScratchCode(t *testing.T) {
	defer tests.PrepareTestEnv(t)()

	session := loginUser(t, "user2")
	key, err := totp.Generate(totp.GenerateOpts{SecretSize: 40, Issuer: "asc-test", AccountName: "user2"})
	require.NoError(t, err)
	tfa := &auth_model.TwoFactor{UID: 2}
	scratch := tfa.GenerateScratchToken()
	require.NoError(t, auth_model.NewTwoFactor(db.DefaultContext, tfa, key.Secret()))

	remove := func(status int) *HTMLDoc {
		req := NewRequestWithValues(t, "POST", "/user/settings/account/delete", map[string]string{
			"username_confirm": "user2",
			"password":         userPassword,
			"passcode":         scratch,
		})
		return NewHTMLParser(t, session.MakeRequest(t, req, status).Body)
	}
	remove(http.StatusSeeOther)
	unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2})
	assert.Contains(t, remove(http.StatusOK).Find(".flash-error").Text(), "The two-factor code is not right")
}

// An account with security keys only has no code to type: a sign-in within the last minutes, which asked the key, stands
// for it; an older one asks to sign in again.
func TestUserDeleteAccountWithSecurityKeyOnly(t *testing.T) {
	defer tests.PrepareTestEnv(t)()

	session := loginUser(t, "user8")
	require.NoError(t, db.Insert(db.DefaultContext, &auth_model.WebAuthnCredential{Name: "key", LowerName: "key", UserID: 8, AttestationType: "none"}))
	remove := func(status int) *HTMLDoc {
		req := NewRequestWithValues(t, "POST", "/user/settings/account/delete", map[string]string{
			"username_confirm": "user8",
			"password":         userPassword,
		})
		return NewHTMLParser(t, session.MakeRequest(t, req, status).Body)
	}

	t.Run("an old sign-in", func(t *testing.T) {
		defer test.MockVariableValue(&security.ReauthFor, 0)()
		doc := NewHTMLParser(t, session.MakeRequest(t, NewRequest(t, "GET", "/user/settings/account"), http.StatusOK).Body)
		doc.AssertElement(t, `[data-testid="delete-signin"]`, true)
		doc.AssertElement(t, `[data-testid="delete-passcode"]`, false)
		assert.Contains(t, remove(http.StatusOK).Find(".flash-error").Text(), "sign out and sign in again with the key")
		unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 8})
	})

	doc := NewHTMLParser(t, session.MakeRequest(t, NewRequest(t, "GET", "/user/settings/account"), http.StatusOK).Body)
	doc.AssertElement(t, `[data-testid="delete-signin"]`, false)
	remove(http.StatusSeeOther)
	assertUserDeleted(t, 8, false)
}

// A recovery code is accepted in place of a code of the app.
func TestUserDeleteAccountWithRecoveryCode(t *testing.T) {
	defer tests.PrepareTestEnv(t)()

	session := loginUser(t, "user8")
	key, err := totp.Generate(totp.GenerateOpts{SecretSize: 40, Issuer: "asc-test", AccountName: "user8"})
	require.NoError(t, err)
	require.NoError(t, auth_model.NewTwoFactor(db.DefaultContext, &auth_model.TwoFactor{UID: 8}, key.Secret()))
	codes, err := auth_model.GenerateRecoveryCodes(db.DefaultContext, 8)
	require.NoError(t, err)
	recovery := codes[0]

	req := NewRequestWithValues(t, "POST", "/user/settings/account/delete", map[string]string{
		"username_confirm": "user8",
		"password":         userPassword,
		"passcode":         recovery,
	})
	session.MakeRequest(t, req, http.StatusSeeOther)
	assertUserDeleted(t, 8, false)
}
