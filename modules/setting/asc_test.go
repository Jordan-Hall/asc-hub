// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package setting

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateAscServerURL(t *testing.T) {
	for raw, want := range map[string]string{
		"https://asc.example.com":        "https://asc.example.com",
		" https://asc.example.com:443/ ": "https://asc.example.com:443",
		"http://127.0.0.1:7890/base/":    "http://127.0.0.1:7890/base",
	} {
		got, err := validateAscServerURL(raw)
		require.NoError(t, err, raw)
		assert.Equal(t, want, got)
	}
	for _, raw := range []string{
		"", "asc.example.com", "ftp://asc.example.com", "https://", "https://u:p@asc.example.com",
		"https://asc.example.com?x=1", "https://asc.example.com?", "https://asc.example.com#f", "javascript:alert(1)",
	} {
		_, err := validateAscServerURL(raw)
		require.Error(t, err, raw)
	}
}

func TestLoadAscFrom(t *testing.T) {
	cfg, err := NewConfigProviderFromData(`
[asc]
ENABLED = true
SERVER_URL = https://asc.example.com/
`)
	require.NoError(t, err)
	loadAscFrom(cfg)
	assert.True(t, Asc.Enabled)
	assert.Equal(t, "https://asc.example.com", Asc.ServerURL)

	cfg, err = NewConfigProviderFromData(`[asc]`)
	require.NoError(t, err)
	loadAscFrom(cfg)
	assert.False(t, Asc.Enabled)
	assert.Empty(t, Asc.ServerURL)
}
