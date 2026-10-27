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

func TestNormaliseAscDownload(t *testing.T) {
	t.Cleanup(func() { Asc.DownloadDir, Asc.DownloadBaseURL = "", "" })
	for _, tc := range []struct{ dir, base, wantBase string }{
		{"", "", ""},
		{"/srv/asc/releases/", "https://dl.example.com/asc", "https://dl.example.com/asc/"},
		{"/srv/asc/releases", " https://dl.example.com/asc/ ", "https://dl.example.com/asc/"},
		{"/srv/asc", "http://127.0.0.1:9000", "http://127.0.0.1:9000/"},
	} {
		Asc.DownloadDir, Asc.DownloadBaseURL = tc.dir, tc.base
		require.NoError(t, normaliseAscDownload(), tc)
		assert.Equal(t, tc.wantBase, Asc.DownloadBaseURL)
	}
	for _, tc := range []struct{ dir, base string }{
		{"relative/dir", ""},
		{"/srv/asc\x00", ""},
		{"", "http://dl.example.com"},
		{"", "https://u:p@dl.example.com"},
		{"", "https://dl.example.com?x=1"},
		{"", "ftp://dl.example.com"},
	} {
		Asc.DownloadDir, Asc.DownloadBaseURL = tc.dir, tc.base
		require.Error(t, normaliseAscDownload(), tc)
	}
}

func TestLoadAscFromDownloadWithoutEnabled(t *testing.T) {
	t.Cleanup(func() { Asc.DownloadDir, Asc.DownloadBaseURL = "", "" })
	cfg, err := NewConfigProviderFromData(`
[asc]
DOWNLOAD_DIR = /srv/asc/releases
DOWNLOAD_BASE_URL = https://dl.example.com/asc
`)
	require.NoError(t, err)
	loadAscFrom(cfg)
	assert.False(t, Asc.Enabled)
	assert.Equal(t, "/srv/asc/releases", Asc.DownloadDir)
	assert.Equal(t, "https://dl.example.com/asc/", Asc.DownloadBaseURL)
}

func TestAscSSHEnabled(t *testing.T) {
	oldSSH, oldAsc := SSH, Asc
	defer func() { SSH, Asc = oldSSH, oldAsc }()

	load := func(data string) {
		t.Helper()
		cfg, err := NewConfigProviderFromData(data)
		require.NoError(t, err)
		SSH.Disabled, SSH.StartBuiltinServer = false, false
		loadAscFrom(cfg)
		loadSSHFrom(cfg)
	}

	// An ASC Hub is HTTPS-only by default, whatever [server] says.
	load("[asc]\nENABLED = true\nSERVER_URL = https://asc.example.com\n[server]\nSTART_SSH_SERVER = true\n")
	assert.False(t, Asc.SSHEnabled)
	assert.True(t, SSH.Disabled)
	assert.False(t, SSH.StartBuiltinServer)
	assert.False(t, SSHEnabled())

	// SSH_ENABLED turns it on, and the server section still decides about the built-in server.
	load("[asc]\nENABLED = true\nSERVER_URL = https://asc.example.com\nSSH_ENABLED = true\n[server]\nSTART_SSH_SERVER = true\n")
	assert.True(t, SSHEnabled())
	assert.True(t, SSH.StartBuiltinServer)

	// SSH_ENABLED does not override DISABLE_SSH.
	load("[asc]\nENABLED = true\nSERVER_URL = https://asc.example.com\nSSH_ENABLED = true\n[server]\nDISABLE_SSH = true\n")
	assert.False(t, SSHEnabled())

	// Without [asc] ENABLED the setting has no effect.
	load("[server]\nSTART_SSH_SERVER = true\n")
	assert.True(t, SSHEnabled())
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

func TestValidateAscPublicURL(t *testing.T) {
	for raw, want := range map[string]string{
		"https://hub.example.org":      "https://hub.example.org",
		" https://hub.example.org/ ":   "https://hub.example.org",
		"https://hub.example.org:8443": "https://hub.example.org:8443",
		"http://127.0.0.1:7890":        "http://127.0.0.1:7890",
		"http://localhost:7890/":       "http://localhost:7890",
		"http://[::1]:7890":            "http://[::1]:7890",
	} {
		got, err := validateAscPublicURL(raw)
		require.NoError(t, err, raw)
		assert.Equal(t, want, got)
	}
	for _, raw := range []string{
		"", "hub.example.org", "http://hub.example.org", "http://10.0.0.5:7890", "ftp://hub.example.org",
		"https://u:p@hub.example.org", "https://hub.example.org?x=1", "https://hub.example.org#f",
	} {
		_, err := validateAscPublicURL(raw)
		require.Error(t, err, raw)
	}
}

func TestLoadAscFromPublicURL(t *testing.T) {
	cfg, err := NewConfigProviderFromData(`
[asc]
ENABLED = true
SERVER_URL = http://127.0.0.1:7890
`)
	require.NoError(t, err)
	loadAscFrom(cfg)
	assert.Empty(t, Asc.PublicURL)
	assert.Equal(t, "http://127.0.0.1:7890", AscPublicURL(), "without PUBLIC_URL the server URL is announced")

	cfg, err = NewConfigProviderFromData(`
[asc]
ENABLED = true
SERVER_URL = http://127.0.0.1:7890
PUBLIC_URL = https://hub.example.org/
`)
	require.NoError(t, err)
	loadAscFrom(cfg)
	assert.Equal(t, "https://hub.example.org", Asc.PublicURL)
	assert.Equal(t, "https://hub.example.org", AscPublicURL())
	assert.Equal(t, "http://127.0.0.1:7890", Asc.ServerURL, "Forgejo still dials the server URL")

	cfg, err = NewConfigProviderFromData(`
[asc]
ENABLED = true
SERVER_URL = http://127.0.0.1:7890
PUBLIC_URL = "  "
`)
	require.NoError(t, err)
	loadAscFrom(cfg)
	assert.Equal(t, "http://127.0.0.1:7890", AscPublicURL(), "a blank PUBLIC_URL is not set")
}
