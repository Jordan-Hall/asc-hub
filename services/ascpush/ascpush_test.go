// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package ascpush

import (
	"crypto/ecdh"
	"crypto/rand"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/webpush"
	"forgejo.org/services/ascnotify"

	_ "forgejo.org/modules/testimport"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMain(m *testing.M) { unittest.MainTest(m) }

type browser struct {
	priv *ecdh.PrivateKey
	auth []byte
}

func newBrowser(t *testing.T) (*browser, *user_model.AscPushSubscription) {
	t.Helper()
	priv, err := ecdh.P256().GenerateKey(rand.Reader)
	require.NoError(t, err)
	auth := make([]byte, 16)
	_, _ = rand.Read(auth)
	return &browser{priv: priv, auth: auth}, &user_model.AscPushSubscription{
		P256dh: webpush.EncodeKey(priv.PublicKey().Bytes()), Auth: webpush.EncodeKey(auth),
	}
}

func TestVAPIDKeyIsMadeOnceAndKept(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	ResetKeysForTest()
	a, err := VAPID(t.Context())
	require.NoError(t, err)
	ResetKeysForTest() // a new process reads the same key from the settings
	b, err := VAPID(t.Context())
	require.NoError(t, err)
	assert.Equal(t, a.Public(), b.Public())
}

func TestSendToDeliversAnEncryptedSignedMessage(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	ResetKeysForTest()
	keys, err := VAPID(t.Context())
	require.NoError(t, err)
	br, sub := newBrowser(t)

	var got struct {
		authz, encoding, ttl string
		body                 []byte
	}
	status := http.StatusCreated
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got.authz, got.encoding, got.ttl = r.Header.Get("Authorization"), r.Header.Get("Content-Encoding"), r.Header.Get("TTL")
		got.body, _ = io.ReadAll(r.Body)
		w.WriteHeader(status)
	}))
	defer srv.Close()
	t.Cleanup(UseClient(srv.Client()))
	sub.Endpoint = srv.URL + "/push/abc"

	res, err := SendTo(t.Context(), keys, sub, Message{Title: "Checks failed", Body: "acme/payments", Path: "/acme/payments/pulls/3"})
	require.NoError(t, err)
	assert.Equal(t, Delivered, res)
	assert.Equal(t, "aes128gcm", got.encoding)
	assert.Equal(t, "86400", got.ttl)
	require.NoError(t, webpush.VerifyAuthorization(got.authz, keys.Public(), srv.URL, time.Now()))
	plain, err := webpush.Decrypt(got.body, br.priv, br.auth)
	require.NoError(t, err)
	var msg Message
	require.NoError(t, json.Unmarshal(plain, &msg))
	assert.Equal(t, Message{Title: "Checks failed", Body: "acme/payments", Path: "/acme/payments/pulls/3"}, msg)

	// a path that leaves the hub is dropped, a long text is cut
	_, err = SendTo(t.Context(), keys, sub, Message{Title: "x", Path: "//evil.example/x", Body: string(make([]byte, 5000))})
	require.NoError(t, err)
	plain, _ = webpush.Decrypt(got.body, br.priv, br.auth)
	msg = Message{}
	require.NoError(t, json.Unmarshal(plain, &msg))
	assert.Empty(t, msg.Path)
	assert.LessOrEqual(t, len(msg.Body), maxTextBytes)

	// the push service forgot the browser, or is unwell
	status = http.StatusGone
	res, err = SendTo(t.Context(), keys, sub, Message{Title: "x"})
	require.NoError(t, err)
	assert.Equal(t, Gone, res)
	status = http.StatusInternalServerError
	res, err = SendTo(t.Context(), keys, sub, Message{Title: "x"})
	require.Error(t, err)
	assert.Equal(t, Failed, res)
	status = http.StatusMovedPermanently
	res, _ = SendTo(t.Context(), keys, sub, Message{Title: "x"})
	assert.Equal(t, Failed, res, "a redirect is not followed")
}

func TestTheSenderNeverConnectsToThisMachine(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	ResetKeysForTest()
	keys, err := VAPID(t.Context())
	require.NoError(t, err)
	_, sub := newBrowser(t)
	called := false
	srv := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }))
	defer srv.Close()
	sub.Endpoint = srv.URL // 127.0.0.1, and the default client of the package
	res, err := SendTo(t.Context(), keys, sub, Message{Title: "x"})
	assert.Equal(t, Failed, res)
	assert.Error(t, err)
	assert.False(t, called)
}

func TestNotifyHonoursTheChoicesOfThePerson(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	ResetKeysForTest()
	const uid = int64(2)
	_, sub := newBrowser(t)
	// the model refuses a loopback endpoint, so the row is written by hand
	sub.UserID, sub.EndpointHash, sub.Endpoint = uid, "h1", "https://push.example.net/x"
	x, err := unittest.GetXORMEngine()
	require.NoError(t, err)
	_, err = x.Insert(sub)
	require.NoError(t, err)

	require.NoError(t, ascnotify.Save(t.Context(), uid, ascnotify.Prefs{PushOff: []string{"checks_failed"}}))
	assert.False(t, ascnotify.PushAllowed(t.Context(), uid, "checks_failed", time.Now()))
	assert.True(t, ascnotify.PushAllowed(t.Context(), uid, "merge_queue", time.Now()))
	// switched off: nothing is looked up, nothing is sent (no goroutine, no VAPID key made)
	Notify(t.Context(), uid, "checks_failed", Message{Title: "x"})
	keysMu.Lock()
	assert.Nil(t, keys)
	keysMu.Unlock()
}
