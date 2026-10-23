// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package setting

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNormalizeMCPOrigin(t *testing.T) {
	for raw, want := range map[string]string{
		"https://Forge.Example.com":      "https://forge.example.com",
		"https://forge.example.com:443":  "https://forge.example.com",
		"http://forge.example.com:80/":   "http://forge.example.com",
		"http://127.0.0.1:3000":          "http://127.0.0.1:3000",
		"https://[::1]:8443":             "https://[::1]:8443",
		"https://[::1]":                  "https://[::1]",
		"HTTPS://forge.example.com:8443": "https://forge.example.com:8443",
	} {
		got, err := NormalizeMCPOrigin(raw)
		require.NoError(t, err, raw)
		assert.Equal(t, want, got, raw)
	}
	for _, raw := range []string{
		"", "null", "*", "https://*.example.com", "ftp://forge.example.com", "https://", "forge.example.com",
		"https://u:p@forge.example.com", "https://forge.example.com/path", "https://forge.example.com?x=1",
		"https://forge.example.com#f", "https://forge.example.com:", "javascript:alert(1)",
	} {
		_, err := NormalizeMCPOrigin(raw)
		require.Error(t, err, raw)
	}
}

func TestLoadMCPFrom(t *testing.T) {
	cfg, err := NewConfigProviderFromData(``)
	require.NoError(t, err)
	loadMCPFrom(cfg)
	assert.True(t, MCP.Enabled)
	assert.False(t, MCP.Write)
	assert.Empty(t, MCP.AllowedOrigins)

	cfg, err = NewConfigProviderFromData(`
[mcp]
ENABLED = false
WRITE = true
ALLOWED_ORIGINS = https://a.example.com:443, http://B.example.com:8080 ,
`)
	require.NoError(t, err)
	loadMCPFrom(cfg)
	assert.False(t, MCP.Enabled)
	assert.True(t, MCP.Write)
	assert.Equal(t, []string{"https://a.example.com", "http://b.example.com:8080"}, MCP.AllowedOrigins)

	// A reload does not keep earlier values.
	cfg, err = NewConfigProviderFromData(``)
	require.NoError(t, err)
	loadMCPFrom(cfg)
	assert.True(t, MCP.Enabled)
	assert.False(t, MCP.Write)
	assert.Empty(t, MCP.AllowedOrigins)
}

func TestParseMCPAllowedOrigins(t *testing.T) {
	_, err := parseMCPAllowedOrigins("https://a.example.com, *")
	require.Error(t, err)

	many := ""
	for i := range MaxMCPAllowedOrigins + 1 {
		many += fmt.Sprintf("https://h%d.example.com,", i)
	}
	_, err = parseMCPAllowedOrigins(many)
	require.Error(t, err)
}
