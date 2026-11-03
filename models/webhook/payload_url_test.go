// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package webhook

import (
	"testing"

	"forgejo.org/models/unittest"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/test"

	"github.com/stretchr/testify/require"
)

// Create and update both refuse an http address unless it is localhost and the instance opted into local http, so an
// entry point that forgets to check (an edit, the API, an organisation or system hook) cannot save one.
func TestWebhookWritesRefuseHTTP(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	defer test.MockVariableValue(&setting.Asc.Enabled, true)()
	defer test.MockVariableValue(&setting.Webhook.AllowedHostList, "loopback,private")()
	defer test.MockVariableValue(&setting.Webhook.AllowLocalHTTP, false)()

	good := &Webhook{RepoID: 1, URL: "https://example.org/hook", HTTPMethod: "POST", ContentType: ContentTypeJSON, Type: "gitea"}
	require.NoError(t, CreateWebhook(t.Context(), good, ""))

	bad := &Webhook{RepoID: 1, URL: "http://example.org/hook", HTTPMethod: "POST", ContentType: ContentTypeJSON, Type: "gitea"}
	require.ErrorIs(t, CreateWebhook(t.Context(), bad, ""), ErrPayloadURLNotHTTPS)
	require.ErrorIs(t, CreateWebhooks(t.Context(), []*Webhook{bad}), ErrPayloadURLNotHTTPS)

	// editing a saved hook to an http address is refused
	good.URL = "http://example.org/hook"
	require.ErrorIs(t, UpdateWebhook(t.Context(), good), ErrPayloadURLNotHTTPS)
	// loopback http is refused until the instance opts in
	good.URL = "http://127.0.0.1:9000/hook"
	require.ErrorIs(t, UpdateWebhook(t.Context(), good), ErrPayloadURLNotHTTPS)
	setting.Webhook.AllowLocalHTTP = true
	require.NoError(t, UpdateWebhook(t.Context(), good))
	for _, local := range []string{"http://localhost:3000/h", "http://[::1]:9000/h"} {
		require.NoError(t, CheckPayloadURL(local), local)
	}
	// a private address is not local: the option does not open plain http to the network
	for _, private := range []string{"http://10.0.0.5/h", "http://192.168.1.2/h", "http://172.16.0.1/h"} {
		require.ErrorIs(t, CheckPayloadURL(private), ErrPayloadURLNotHTTPS, private)
	}
	// the list still decides: loopback the instance does not allow is refused
	setting.Webhook.AllowedHostList = "external"
	require.ErrorIs(t, CheckPayloadURL("http://127.0.0.1:9000/h"), ErrPayloadURLNotHTTPS)
	setting.Webhook.AllowedHostList = "loopback,private"
	// the stored address is what was last accepted
	got, err := GetWebhookByID(t.Context(), good.ID)
	require.NoError(t, err)
	require.Equal(t, "http://127.0.0.1:9000/hook", got.URL)

	// without asc nothing changes
	setting.Asc.Enabled = false
	good.URL = "http://example.org/hook"
	require.NoError(t, UpdateWebhook(t.Context(), good))
}
