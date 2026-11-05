// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package integration

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	auth_model "forgejo.org/models/auth"
	"forgejo.org/models/db"
	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"
	wa "forgejo.org/modules/auth/webauthn"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/test"
	"forgejo.org/tests"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/protocol/webauthncbor"
	"github.com/go-webauthn/webauthn/protocol/webauthncose"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// passkey is a software authenticator: one P-256 key kept for one account of the hub.
type passkey struct {
	key    *ecdsa.PrivateKey
	id     []byte
	handle []byte
}

func newPasskey(t *testing.T, u *user_model.User) *passkey {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	pk := &passkey{key: key, id: []byte("passkey-of-" + u.Name), handle: (*wa.User)(u).WebAuthnID()}
	pub, err := key.PublicKey.ECDH()
	require.NoError(t, err)
	raw := pub.Bytes() // 0x04 || x || y
	cose, err := webauthncbor.Marshal(webauthncose.EC2PublicKeyData{
		PublicKeyData: webauthncose.PublicKeyData{KeyType: int64(webauthncose.EllipticKey), Algorithm: int64(webauthncose.AlgES256)},
		Curve:         1, // P-256
		XCoord:        raw[1:33],
		YCoord:        raw[33:],
	})
	require.NoError(t, err)
	_, err = db.GetEngine(db.DefaultContext).Insert(&auth_model.WebAuthnCredential{
		Name: "passkey", UserID: u.ID, CredentialID: pk.id, PublicKey: cose, AttestationType: "none",
	})
	require.NoError(t, err)
	return pk
}

// answer signs challenge (base64url, as the hub sent it). uv says whether the person was verified.
func (pk *passkey) answer(t *testing.T, challenge string, uv bool, counter uint32) map[string]any {
	t.Helper()
	b64 := base64.RawURLEncoding.EncodeToString
	origin, err := protocol.FullyQualifiedOrigin(setting.AppURL)
	require.NoError(t, err)
	clientData, err := json.Marshal(map[string]string{"type": "webauthn.get", "challenge": challenge, "origin": origin})
	require.NoError(t, err)
	rpHash := sha256.Sum256([]byte(setting.Domain))
	flags := byte(protocol.FlagUserPresent)
	if uv {
		flags |= byte(protocol.FlagUserVerified)
	}
	authData := append(rpHash[:], flags)
	authData = binary.BigEndian.AppendUint32(authData, counter)
	clientHash := sha256.Sum256(clientData)
	digest := sha256.Sum256(append(append([]byte{}, authData...), clientHash[:]...))
	sig, err := ecdsa.SignASN1(rand.Reader, pk.key, digest[:])
	require.NoError(t, err)
	return map[string]any{
		"id":    b64(pk.id),
		"rawId": b64(pk.id),
		"type":  "public-key",
		"response": map[string]string{
			"authenticatorData": b64(authData),
			"clientDataJSON":    b64(clientData),
			"signature":         b64(sig),
			"userHandle":        b64(pk.handle),
		},
	}
}

// challenge starts a passkey sign in in session and returns its challenge.
func passkeyChallenge(t *testing.T, session *TestSession) string {
	t.Helper()
	resp := session.MakeRequest(t, NewRequest(t, "GET", "/user/webauthn/passkey/assertion"), http.StatusOK)
	var got struct {
		PublicKey struct {
			Challenge        string `json:"challenge"`
			UserVerification string `json:"userVerification"`
			AllowCredentials []any  `json:"allowCredentials"`
		} `json:"publicKey"`
	}
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &got))
	// a discoverable sign in names no account and asks the key to verify the person
	assert.Empty(t, got.PublicKey.AllowCredentials)
	assert.Equal(t, "required", got.PublicKey.UserVerification)
	require.NotEmpty(t, got.PublicKey.Challenge)
	return got.PublicKey.Challenge
}

// TestAscPasskeySignIn checks Sign in with a passkey (board Onboarding): the button under the sign in form, the
// discoverable challenge, and the sign in itself, which needs a verified person, a key of a live account and a fresh
// challenge.
func TestAscPasskeySignIn(t *testing.T) {
	defer tests.PrepareTestEnv(t)()
	defer test.MockVariableValue(&setting.Asc.Enabled, true)()

	user2 := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2})
	key2 := newPasskey(t, user2)

	t.Run("Button", func(t *testing.T) {
		doc := NewHTMLParser(t, MakeRequest(t, NewRequest(t, "GET", "/user/login"), http.StatusOK).Body)
		doc.AssertElement(t, "button[data-asc-passkey]", true)
		assert.Contains(t, doc.Find("button[data-asc-passkey]").Text(), "Sign in with a passkey")
		doc.AssertElement(t, ".ds-or", true)
	})

	t.Run("SignIn", func(t *testing.T) {
		session := emptyTestSession(t)
		challenge := passkeyChallenge(t, session)
		resp := session.MakeRequest(t, NewRequestWithJSON(t, "POST", "/user/webauthn/passkey/login", key2.answer(t, challenge, true, 1)), http.StatusOK)
		assert.Contains(t, resp.Body.String(), `"redirect"`)
		session.MakeRequest(t, NewRequest(t, "GET", "/user/settings"), http.StatusOK)
		cred := unittest.AssertExistsAndLoadBean(t, &auth_model.WebAuthnCredential{UserID: user2.ID, Name: "passkey"})
		assert.EqualValues(t, 1, cred.SignCount)
		assert.False(t, cred.Legacy)
	})

	t.Run("Remember", func(t *testing.T) {
		session := emptyTestSession(t)
		challenge := passkeyChallenge(t, session)
		resp := session.MakeRequest(t, NewRequestWithJSON(t, "POST", "/user/webauthn/passkey/login?remember=on", key2.answer(t, challenge, true, 2)), http.StatusOK)
		assert.Contains(t, strings.Join(resp.Header().Values("Set-Cookie"), ";"), setting.CookieRememberName+"=")
	})

	t.Run("NotVerified", func(t *testing.T) {
		session := emptyTestSession(t)
		challenge := passkeyChallenge(t, session)
		session.MakeRequest(t, NewRequestWithJSON(t, "POST", "/user/webauthn/passkey/login", key2.answer(t, challenge, false, 3)), http.StatusForbidden)
		session.MakeRequest(t, NewRequest(t, "GET", "/user/settings"), http.StatusSeeOther)
	})

	t.Run("OneAnswerPerChallenge", func(t *testing.T) {
		// a refused answer uses the challenge up: a good answer to it is refused too
		session := emptyTestSession(t)
		challenge := passkeyChallenge(t, session)
		session.MakeRequest(t, NewRequestWithJSON(t, "POST", "/user/webauthn/passkey/login", key2.answer(t, challenge, false, 4)), http.StatusForbidden)
		session.MakeRequest(t, NewRequestWithJSON(t, "POST", "/user/webauthn/passkey/login", key2.answer(t, challenge, true, 4)), http.StatusForbidden)
		session.MakeRequest(t, NewRequest(t, "GET", "/user/settings"), http.StatusSeeOther)
	})

	t.Run("NoChallenge", func(t *testing.T) {
		session := emptyTestSession(t)
		session.MakeRequest(t, NewRequestWithJSON(t, "POST", "/user/webauthn/passkey/login", key2.answer(t, "AAAA", true, 5)), http.StatusForbidden)
	})

	t.Run("WrongAccount", func(t *testing.T) {
		// the key of user2 claiming to be user5: the credential is not one of user5's
		user5 := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 5})
		forged := *key2
		forged.handle = (*wa.User)(user5).WebAuthnID()
		session := emptyTestSession(t)
		challenge := passkeyChallenge(t, session)
		session.MakeRequest(t, NewRequestWithJSON(t, "POST", "/user/webauthn/passkey/login", forged.answer(t, challenge, true, 6)), http.StatusForbidden)

		forged.handle = []byte("not-a-handle")
		challenge = passkeyChallenge(t, session)
		session.MakeRequest(t, NewRequestWithJSON(t, "POST", "/user/webauthn/passkey/login", forged.answer(t, challenge, true, 7)), http.StatusForbidden)
	})

	t.Run("Prohibited", func(t *testing.T) {
		user4 := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 4})
		key4 := newPasskey(t, user4)
		user4.ProhibitLogin = true
		require.NoError(t, user_model.UpdateUserCols(db.DefaultContext, user4, "prohibit_login"))
		session := emptyTestSession(t)
		challenge := passkeyChallenge(t, session)
		session.MakeRequest(t, NewRequestWithJSON(t, "POST", "/user/webauthn/passkey/login", key4.answer(t, challenge, true, 1)), http.StatusForbidden)
	})

	t.Run("Register", func(t *testing.T) {
		// user1 has no key, so the password signs in
		session := loginUser(t, "user1")
		resp := session.MakeRequest(t, NewRequestWithValues(t, "POST", "/user/settings/security/webauthn/request_register", map[string]string{"name": "laptop"}), http.StatusOK)
		var got struct {
			PublicKey struct {
				AuthenticatorSelection struct {
					ResidentKey string `json:"residentKey"`
				} `json:"authenticatorSelection"`
			} `json:"publicKey"`
		}
		require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &got))
		assert.Equal(t, "preferred", got.PublicKey.AuthenticatorSelection.ResidentKey)
	})

	t.Run("AscOff", func(t *testing.T) {
		defer test.MockVariableValue(&setting.Asc.Enabled, false)()
		doc := NewHTMLParser(t, MakeRequest(t, NewRequest(t, "GET", "/user/login"), http.StatusOK).Body)
		doc.AssertElement(t, "button[data-asc-passkey]", false)
		MakeRequest(t, NewRequest(t, "GET", "/user/webauthn/passkey/assertion"), http.StatusNotFound)
		MakeRequest(t, NewRequestWithJSON(t, "POST", "/user/webauthn/passkey/login", map[string]string{}), http.StatusNotFound)
	})
}
