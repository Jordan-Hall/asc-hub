// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package web

import (
	"net/http"
	"testing"

	"forgejo.org/modules/json"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/test"
	"forgejo.org/services/contexttest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAscWellKnown(t *testing.T) {
	t.Run("disabled", func(t *testing.T) {
		defer test.MockVariableValue(&setting.Asc.Enabled, false)()
		defer test.MockVariableValue(&setting.Asc.ServerURL, "https://asc.example.com")()
		ctx, resp := contexttest.MockContext(t, "GET /.well-known/asc")
		AscWellKnown(ctx)
		assert.Equal(t, http.StatusNotFound, resp.Code)
	})

	t.Run("enabled without a server URL", func(t *testing.T) {
		defer test.MockVariableValue(&setting.Asc.Enabled, true)()
		defer test.MockVariableValue(&setting.Asc.ServerURL, "")()
		ctx, resp := contexttest.MockContext(t, "GET /.well-known/asc")
		AscWellKnown(ctx)
		assert.Equal(t, http.StatusNotFound, resp.Code)
	})

	t.Run("enabled", func(t *testing.T) {
		defer test.MockVariableValue(&setting.Asc.Enabled, true)()
		defer test.MockVariableValue(&setting.Asc.ServerURL, "https://asc.example.com:443")()
		ctx, resp := contexttest.MockContext(t, "GET /.well-known/asc")
		AscWellKnown(ctx)
		require.Equal(t, http.StatusOK, resp.Code)
		var doc map[string]any
		require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &doc))
		assert.Equal(t, map[string]any{"server": "https://asc.example.com:443", "repos": "*"}, doc)
	})

	t.Run("announces the public URL, not the one Forgejo dials", func(t *testing.T) {
		defer test.MockVariableValue(&setting.Asc.Enabled, true)()
		defer test.MockVariableValue(&setting.Asc.ServerURL, "http://127.0.0.1:7890")()
		defer test.MockVariableValue(&setting.Asc.PublicURL, "https://hub.example.org")()
		ctx, resp := contexttest.MockContext(t, "GET /.well-known/asc")
		AscWellKnown(ctx)
		require.Equal(t, http.StatusOK, resp.Code)
		var doc map[string]any
		require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &doc))
		assert.Equal(t, map[string]any{"server": "https://hub.example.org", "repos": "*"}, doc)
	})
}
