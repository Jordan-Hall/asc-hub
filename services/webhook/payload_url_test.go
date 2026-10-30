// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package webhook

import (
	"testing"

	"forgejo.org/modules/setting"
	"forgejo.org/modules/test"

	"github.com/stretchr/testify/require"
)

func TestCheckPayloadURL(t *testing.T) {
	defer test.MockVariableValue(&setting.Asc.Enabled, true)()
	defer test.MockVariableValue(&setting.Webhook.AllowedHostList, "loopback,private")()
	for _, ok := range []string{"https://example.org/hook", "http://127.0.0.1:8080/h", "http://localhost/h", "http://10.0.0.5/h", "not a url"} {
		require.NoError(t, CheckPayloadURL(ok), ok)
	}
	for _, bad := range []string{"http://example.org/hook", "http://8.8.8.8/h", "ftp://127.0.0.1/h", "http://internal.corp/h"} {
		require.ErrorIs(t, CheckPayloadURL(bad), ErrPayloadURLNotHTTPS, bad)
	}
	// the default list is external only: a loopback address is not reachable, so http for it cannot be saved
	setting.Webhook.AllowedHostList = ""
	require.ErrorIs(t, CheckPayloadURL("http://127.0.0.1/h"), ErrPayloadURLNotHTTPS)
	require.NoError(t, CheckPayloadURL("https://127.0.0.1/h"))
	// without asc nothing changes
	setting.Asc.Enabled = false
	require.NoError(t, CheckPayloadURL("http://example.org/hook"))
}
