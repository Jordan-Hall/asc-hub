// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package setting

import (
	"crypto/ecdh"
	"crypto/rand"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/webpush"
	"forgejo.org/services/contexttest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func subscribeBody(t *testing.T, endpoint, p256dh, auth string) string {
	t.Helper()
	b, err := json.Marshal(map[string]any{"endpoint": endpoint, "keys": map[string]string{"p256dh": p256dh, "auth": auth}})
	require.NoError(t, err)
	return string(b)
}

func TestPushSubscribeChecksWhatABrowserSends(t *testing.T) {
	unittest.PrepareTestEnv(t)
	ua, err := ecdh.P256().GenerateKey(rand.Reader)
	require.NoError(t, err)
	key := webpush.EncodeKey(ua.PublicKey().Bytes())
	auth := webpush.EncodeKey(make([]byte, 16))

	do := func(body string) int {
		ctx, resp := contexttest.MockContext(t, "POST /user/settings/notifications/push")
		contexttest.LoadUser(t, ctx, 2)
		ctx.Req.Body = io.NopCloser(strings.NewReader(body))
		PushSubscribe(ctx)
		return resp.Code
	}
	assert.Equal(t, http.StatusCreated, do(subscribeBody(t, "https://push.example.net/abc", key, auth)))
	subs, err := user_model.ListAscPushSubscriptions(t.Context(), 2)
	require.NoError(t, err)
	assert.Len(t, subs, 1)

	for name, body := range map[string]string{
		"http":              subscribeBody(t, "http://push.example.net/abc", key, auth),
		"loopback":          subscribeBody(t, "https://127.0.0.1/abc", key, auth),
		"cloud metadata":    subscribeBody(t, "https://169.254.169.254/latest/meta-data", key, auth),
		"private network":   subscribeBody(t, "https://10.1.2.3/x", key, auth),
		"not a point":       subscribeBody(t, "https://push.example.net/b", webpush.EncodeKey(make([]byte, 65)), auth),
		"short auth":        subscribeBody(t, "https://push.example.net/b", key, webpush.EncodeKey(make([]byte, 4))),
		"not json":          "{",
		"unknown fields ok": `{"endpoint":"x"}`,
	} {
		assert.Equal(t, http.StatusBadRequest, do(body), name)
	}
	subs, _ = user_model.ListAscPushSubscriptions(t.Context(), 2)
	assert.Len(t, subs, 1, "nothing was added by the refused ones")

	// removing: only the own subscription
	ctx, resp := contexttest.MockContext(t, "POST /user/settings/notifications/push/remove")
	contexttest.LoadUser(t, ctx, 4)
	ctx.Req.Body = io.NopCloser(strings.NewReader(`{"endpoint":"https://push.example.net/abc"}`))
	PushUnsubscribe(ctx)
	assert.Equal(t, http.StatusOK, resp.Code)
	subs, _ = user_model.ListAscPushSubscriptions(t.Context(), 2)
	assert.Len(t, subs, 1, "another person cannot remove it")
	ctx, resp = contexttest.MockContext(t, "POST /user/settings/notifications/push/remove")
	contexttest.LoadUser(t, ctx, 2)
	ctx.Req.Body = io.NopCloser(strings.NewReader(`{"endpoint":"https://push.example.net/abc"}`))
	PushUnsubscribe(ctx)
	subs, _ = user_model.ListAscPushSubscriptions(t.Context(), 2)
	assert.Empty(t, subs)
}
