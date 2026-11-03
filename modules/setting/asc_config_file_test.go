// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package setting

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReadConfigFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.ini")
	require.NoError(t, os.WriteFile(path, []byte("\ufeffAPP_NAME = nova\n; a comment\n\n[server]\nDOMAIN = hub.example.org\n# another\nSSH_PORT: 2222\n[mailer]\nFROM = \"asc@example.org\"\n"), 0o600))
	f, err := ReadConfigFile(path)
	require.NoError(t, err)

	e, ok := f.Find("APP_NAME")
	require.True(t, ok)
	assert.Equal(t, ConfigFileEntry{Section: "DEFAULT", Key: "APP_NAME", Value: "nova", Line: 1}, e)
	assert.Equal(t, "APP_NAME", e.Name())

	e, ok = f.Find("server.domain")
	require.True(t, ok, "names are found in any case")
	assert.Equal(t, 5, e.Line)
	assert.Equal(t, "server.DOMAIN", e.Name())

	e, ok = f.Find("server.DISABLE_SSH", "server.SSH_PORT")
	require.True(t, ok, "the first name the file sets")
	assert.Equal(t, 7, e.Line)
	assert.Equal(t, "2222", e.Value)

	_, ok = f.Find("mailer.SMTP_ADDR")
	assert.False(t, ok)
	_, ok = (*ConfigFile)(nil).Find("server.DOMAIN")
	assert.False(t, ok)
}

func TestChangedConfigKeys(t *testing.T) {
	dir := t.TempDir()
	write := func(body string) *ConfigFile {
		path := filepath.Join(dir, "app.ini")
		require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
		f, err := ReadConfigFile(path)
		require.NoError(t, err)
		return f
	}
	before := write("[server]\nDOMAIN = a.example.org\nSSH_PORT = 22\n[mailer]\nFROM = x@example.org\n")
	assert.Empty(t, ChangedConfigKeys(before, before))

	after := write("[server]\nDOMAIN = b.example.org\nSSH_PORT = 22\nPROTOCOL = https\n")
	assert.Equal(t, []string{"server.DOMAIN", "server.PROTOCOL", "mailer.FROM"}, ChangedConfigKeys(before, after), "changed and added in file order, removed last")
	assert.Empty(t, ChangedConfigKeys(nil, after), "with no file read at start nothing counts as changed")
}

func TestConfigFileNow(t *testing.T) {
	old := CustomConf
	t.Cleanup(func() { CustomConf = old; RememberConfigFile() })
	CustomConf = filepath.Join(t.TempDir(), "app.ini")
	require.NoError(t, os.WriteFile(CustomConf, []byte("[server]\nDOMAIN = a.example.org\n"), 0o600))
	RememberConfigFile()

	f, changed, err := ConfigFileNow()
	require.NoError(t, err)
	assert.Empty(t, changed)
	assert.Equal(t, CustomConf, f.Path)

	require.NoError(t, os.WriteFile(CustomConf, []byte("[server]\nDOMAIN = b.example.org\n"), 0o600))
	_, changed, err = ConfigFileNow()
	require.NoError(t, err)
	assert.Equal(t, []string{"server.DOMAIN"}, changed)
}
