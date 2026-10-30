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

// Create and update both refuse an http address that is not a loopback or private target the instance allows, so an
// entry point that forgets to check (an edit, the API, an organisation or system hook) cannot save one.
func TestWebhookWritesRefuseHTTP(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	defer test.MockVariableValue(&setting.Asc.Enabled, true)()
	defer test.MockVariableValue(&setting.Webhook.AllowedHostList, "loopback")()

	good := &Webhook{RepoID: 1, URL: "https://example.org/hook", HTTPMethod: "POST", ContentType: ContentTypeJSON, Type: "gitea"}
	require.NoError(t, CreateWebhook(t.Context(), good, ""))

	bad := &Webhook{RepoID: 1, URL: "http://example.org/hook", HTTPMethod: "POST", ContentType: ContentTypeJSON, Type: "gitea"}
	require.ErrorIs(t, CreateWebhook(t.Context(), bad, ""), ErrPayloadURLNotHTTPS)
	require.ErrorIs(t, CreateWebhooks(t.Context(), []*Webhook{bad}), ErrPayloadURLNotHTTPS)

	// editing a saved hook to an http address is refused
	good.URL = "http://example.org/hook"
	require.ErrorIs(t, UpdateWebhook(t.Context(), good), ErrPayloadURLNotHTTPS)
	// a loopback http address the list allows is accepted
	good.URL = "http://127.0.0.1:9000/hook"
	require.NoError(t, UpdateWebhook(t.Context(), good))
	// the stored address is what was last accepted
	got, err := GetWebhookByID(t.Context(), good.ID)
	require.NoError(t, err)
	require.Equal(t, "http://127.0.0.1:9000/hook", got.URL)

	// without asc nothing changes
	setting.Asc.Enabled = false
	good.URL = "http://example.org/hook"
	require.NoError(t, UpdateWebhook(t.Context(), good))
}
