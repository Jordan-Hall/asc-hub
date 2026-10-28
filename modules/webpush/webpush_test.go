// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package webpush

import (
	"crypto/ecdh"
	"crypto/rand"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func must(t *testing.T, s string) []byte {
	t.Helper()
	b, err := DecodeKey(s)
	require.NoError(t, err)
	return b
}

// The example of RFC 8291 appendix A.
func TestEncryptMatchesRFC8291AppendixA(t *testing.T) {
	plaintext := []byte("When I grow up, I want to be a watermelon")
	asPrivate, err := ecdh.P256().NewPrivateKey(must(t, "yfWPiYE-n46HLnH0KqZOF1fJJU3MYrct3AELtAQ-oRw"))
	require.NoError(t, err)
	assert.Equal(t, must(t, "BP4z9KsN6nGRTbVYI_c7VJSPQTBtkgcy27mlmlMoZIIgDll6e3vCYLocInmYWAmS6TlzAC8wEqKK6PBru3jl7A8"), asPrivate.PublicKey().Bytes())
	uaPublic := must(t, "BCVxsr7N_eNgVRqvHtD0zTZsEc6-VV-JvLexhqUzORcxaOzi6-AYWXvTBHm4bjyPjs7Vd8pZGH6SRpkNtoIAiw4")
	auth := must(t, "BTBZMqHH6r4Tts7J_aSIgg")
	salt := must(t, "DGv6ra1nlYgDCS1FRnbzlw")

	got, err := Encrypt(plaintext, uaPublic, auth, salt, asPrivate)
	require.NoError(t, err)
	want := must(t, "DGv6ra1nlYgDCS1FRnbzlwAAEABBBP4z9KsN6nGRTbVYI_c7VJSPQTBtkgcy27mlmlMoZIIgDll6e3vCYLocInmYWAmS6TlzAC8wEqKK6PBru3jl7A_yl95bQpu6cVPTpK4Mqgkf1CXztLVBSt2Ks3oZwbuwXPXLWyouBWLVWGNWQexSgSxsj_Qulcy4a-fN")
	assert.Equal(t, want, got)

	// and the browser side reads the example back
	uaPrivate, err := ecdh.P256().NewPrivateKey(must(t, "q1dXpw3UpT5VOmu_cf_v6ih07Aems3njxI-JWgLcM94"))
	require.NoError(t, err)
	back, err := Decrypt(want, uaPrivate, auth)
	require.NoError(t, err)
	assert.Equal(t, plaintext, back)
}

func TestEncryptRoundTripWithFreshKeys(t *testing.T) {
	ua, err := ecdh.P256().GenerateKey(rand.Reader)
	require.NoError(t, err)
	auth := make([]byte, 16)
	_, _ = rand.Read(auth)
	a, err := Encrypt([]byte(`{"title":"x"}`), ua.PublicKey().Bytes(), auth, nil, nil)
	require.NoError(t, err)
	b, err := Encrypt([]byte(`{"title":"x"}`), ua.PublicKey().Bytes(), auth, nil, nil)
	require.NoError(t, err)
	assert.NotEqual(t, a, b, "a fresh salt and key for every message")
	back, err := Decrypt(a, ua, auth)
	require.NoError(t, err)
	assert.Equal(t, `{"title":"x"}`, string(back))

	// a changed byte, a wrong secret and a longer message are refused
	a[len(a)-1] ^= 1
	_, err = Decrypt(a, ua, auth)
	assert.Error(t, err)
	_, err = Encrypt(make([]byte, MaxPayload+1), ua.PublicKey().Bytes(), auth, nil, nil)
	assert.Error(t, err)
	_, err = Encrypt(make([]byte, MaxPayload), ua.PublicKey().Bytes(), auth, nil, nil)
	assert.NoError(t, err)
	_, err = Encrypt([]byte("x"), []byte("not a key"), auth, nil, nil)
	assert.Error(t, err)
	_, err = Encrypt([]byte("x"), ua.PublicKey().Bytes(), auth[:8], nil, nil)
	assert.Error(t, err)
}

func TestValidateSubscriptionKeys(t *testing.T) {
	ua, _ := ecdh.P256().GenerateKey(rand.Reader)
	good := EncodeKey(ua.PublicKey().Bytes())
	auth := EncodeKey(make([]byte, 16))
	require.NoError(t, ValidateSubscriptionKeys(good, auth))
	assert.Error(t, ValidateSubscriptionKeys("!!", auth))
	assert.Error(t, ValidateSubscriptionKeys(EncodeKey(make([]byte, 65)), auth), "not a point on the curve")
	assert.Error(t, ValidateSubscriptionKeys(good, EncodeKey(make([]byte, 15))))
}

func TestVAPID(t *testing.T) {
	k, err := NewKeys()
	require.NoError(t, err)
	k2, err := KeysFromPrivate(k.Private())
	require.NoError(t, err)
	assert.Equal(t, k.Public(), k2.Public())
	assert.Len(t, k.Public(), 65)

	now := time.Unix(1_800_000_000, 0)
	h, err := k.Authorization("https://push.example.net:8443/send/abc", "mailto:admin@example.org", now)
	require.NoError(t, err)
	require.NoError(t, VerifyAuthorization(h, k.Public(), "https://push.example.net:8443", now))
	assert.Error(t, VerifyAuthorization(h, k.Public(), "https://other.example.net", now), "the token is for one push service")
	assert.Error(t, VerifyAuthorization(h, k.Public(), "https://push.example.net:8443", now.Add(13*time.Hour)), "and for 12 hours")
	other, _ := NewKeys()
	assert.Error(t, VerifyAuthorization(h, other.Public(), "https://push.example.net:8443", now))

	_, err = k.Authorization("http://push.example.net/x", "mailto:a@b.c", now)
	assert.Error(t, err, "https only")
	_, err = KeysFromPrivate(make([]byte, 32))
	assert.Error(t, err)
}
