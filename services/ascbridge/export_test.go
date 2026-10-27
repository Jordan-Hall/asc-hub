// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package ascbridge

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeAscBinary writes a script that appends its arguments, one call per line, to the returned log file.
func fakeAscBinary(t *testing.T) (binary, log string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the fake asc binary is a shell script")
	}
	dir := t.TempDir()
	log = filepath.Join(dir, "calls.log")
	binary = filepath.Join(dir, "asc")
	script := "#!/bin/sh\necho \"$*\" >> " + log + "\nif [ \"$1\" = clone ]; then\n  for last; do :; done\n  mkdir -p \"$last\"\nfi\n"
	require.NoError(t, os.WriteFile(binary, []byte(script), 0o755))
	return binary, log
}

func TestExportKeepsPositionalsOutOfTheOptions(t *testing.T) {
	binary, log := fakeAscBinary(t)
	e := &CLIExporter{Binary: binary, Workspaces: t.TempDir(), ServerURL: "https://asc.example/", Token: "tok-for-the-test"}

	require.NoError(t, e.Export(t.Context(), "acme/payments", t.TempDir()))
	raw, err := os.ReadFile(log)
	require.NoError(t, err)
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	require.NotEmpty(t, lines)
	assert.Equal(t, "clone -- https://asc.example/acme/payments payments", lines[0], "the clone ends its options before the positionals")
}

func TestExportRefusesNamesThatLookLikeOptions(t *testing.T) {
	binary, log := fakeAscBinary(t)
	e := &CLIExporter{Binary: binary, Workspaces: t.TempDir(), ServerURL: "https://asc.example", Token: "tok-for-the-test"}
	for _, name := range []string{"acme/--upload-pack=x", "-acme/repo", "acme/-repo", "acme", "acme/../x", "/repo"} {
		err := e.Export(t.Context(), name, t.TempDir())
		assert.Error(t, err, name)
	}
	_, err := os.Stat(log)
	assert.True(t, os.IsNotExist(err), "asc is never started for a refused name")
}
