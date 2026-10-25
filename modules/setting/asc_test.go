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
	assert.Equal(t, DefaultAscShadowSamplePercent, Asc.ShadowSamplePercent)
	assert.Equal(t, 10, Asc.ShadowSamplePercent)

	cfg, err = NewConfigProviderFromData(`
[asc]
ENABLED = true
SERVER_URL = https://asc.example.com/
SHADOW_SAMPLE_PERCENT = 25
`)
	require.NoError(t, err)
	loadAscFrom(cfg)
	assert.Equal(t, 25, Asc.ShadowSamplePercent)

	cfg, err = NewConfigProviderFromData(`
[asc]
ENABLED = true
SERVER_URL = https://asc.example.com/
SHADOW_SAMPLE_PERCENT = 0
`)
	require.NoError(t, err)
	loadAscFrom(cfg)
	assert.Zero(t, Asc.ShadowSamplePercent, "zero turns the shadow off")

	cfg, err = NewConfigProviderFromData(`[asc]`)
	require.NoError(t, err)
	loadAscFrom(cfg)
	assert.False(t, Asc.Enabled)
	assert.Empty(t, Asc.ServerURL)
}

func TestLoadAscFromWriterAndBridge(t *testing.T) {
	cfg, err := NewConfigProviderFromData(`
[asc]
ENABLED = true
SERVER_URL = https://asc.example.com/
`)
	require.NoError(t, err)
	loadAscFrom(cfg)
	assert.Equal(t, AscWriterGit, Asc.Writer, "writes stay on git until Stage 4 is chosen")
	assert.Equal(t, AscBridgeEager, Asc.Bridge)
	assert.Equal(t, "asc", Asc.AscBinary)
	assert.Equal(t, AscWriterGit, Asc.NewRepositories)

	cfg, err = NewConfigProviderFromData(`
[asc]
ENABLED = true
SERVER_URL = https://asc.example.com/
WRITER = ASC
BRIDGE = Lazy
ASC_BINARY = /usr/local/bin/asc
BRIDGE_WORKSPACES = /var/lib/forgejo/asc-bridge
NEW_REPOSITORIES = asc
`)
	require.NoError(t, err)
	loadAscFrom(cfg)
	assert.Equal(t, AscWriterAsc, Asc.Writer)
	assert.Equal(t, AscBridgeLazy, Asc.Bridge)
	assert.Equal(t, "/usr/local/bin/asc", Asc.AscBinary)
	assert.Equal(t, "/var/lib/forgejo/asc-bridge", Asc.BridgeWorkspaces)
	assert.Equal(t, AscWriterAsc, Asc.NewRepositories)
}
