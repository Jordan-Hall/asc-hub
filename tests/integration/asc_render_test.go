// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package integration

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"forgejo.org/modules/setting"
	"forgejo.org/tests"

	"github.com/stretchr/testify/require"
)

// TestAscRenderPages is the render harness of the ASC Hub design pass, not a regression test: when
// ASC_RENDER_DIR names a directory it fetches the pages of ASC_RENDER_PAGES ("name=path[@user]" pairs
// separated by commas; @user signs that user in, nothing is anonymous) from the real application with the
// fixture data, and writes the HTML of each to <dir>/<name>.html so a browser can screenshot it. Without
// the variable it does nothing.
func TestAscRenderPages(t *testing.T) {
	dir := os.Getenv("ASC_RENDER_DIR")
	if dir == "" {
		t.Skip("ASC_RENDER_DIR is not set")
	}
	defer tests.PrepareTestEnv(t)()
	defer enableDemoPages()() // /-/demo/error/500 renders the 500 page
	require.NoError(t, os.MkdirAll(dir, 0o755))

	release := t.TempDir()
	writeAscRelease(t, release)
	defer func(old string) { setting.Asc.DownloadDir = old }(setting.Asc.DownloadDir)
	setting.Asc.DownloadDir = release

	pages := os.Getenv("ASC_RENDER_PAGES")
	if pages == "" {
		pages = "home=/,signin=/user/login,signup=/user/sign_up,explore=/explore/repos,download=/download,notfound=/nope/nope/nope"
	}
	sessions := map[string]*TestSession{"": emptyTestSession(t)}
	for _, spec := range strings.Split(pages, ",") {
		name, rest, ok := strings.Cut(spec, "=")
		require.True(t, ok, spec)
		path, user, _ := strings.Cut(rest, "@")
		session, found := sessions[user]
		if !found {
			session = loginUser(t, user)
			sessions[user] = session
		}
		req := NewRequest(t, "GET", path).SetHeader("Accept", "text/html")
		if ua := os.Getenv("ASC_RENDER_UA"); ua != "" {
			req.SetHeader("User-Agent", ua)
		}
		resp := session.MakeRequest(t, req, NoExpectedStatus)
		require.NoError(t, os.WriteFile(filepath.Join(dir, name+".html"), resp.Body.Bytes(), 0o644))
		t.Logf("%s: %d %s", name, resp.Code, http.StatusText(resp.Code))
	}
}

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// writeAscRelease writes a manifest and the files it lists into dir.
func writeAscRelease(t *testing.T, dir string) {
	t.Helper()
	files := []struct{ name, os, arch, kind, sig string }{
		{"asc-1.4.0-x86_64-pc-windows-msvc.msi", "windows", "x86_64", "installer", ""},
		{"asc-1.4.0-x86_64-pc-windows-msvc.zip", "windows", "x86_64", "archive", ""},
		{"asc-1.4.0-x86_64-unknown-linux-gnu.tar.gz", "linux", "x86_64", "archive", "asc-1.4.0-x86_64-unknown-linux-gnu.tar.gz.minisig"},
		{"install.sh", "linux", "any", "script", ""},
		{"asc-1.4.0-aarch64-apple-darwin.tar.gz", "macos", "arm64", "archive", ""},
	}
	var entries []string
	for _, f := range files {
		body := []byte("asc test file " + f.name)
		require.NoError(t, os.WriteFile(filepath.Join(dir, f.name), body, 0o600))
		entry := `{"name":"` + f.name + `","os":"` + f.os + `","arch":"` + f.arch + `","kind":"` + f.kind + `","sha256":"` + sha256Hex(body) + `","size":` + strconv.Itoa(len(body))
		if f.sig != "" {
			require.NoError(t, os.WriteFile(filepath.Join(dir, f.sig), []byte("sig"), 0o600))
			entry += `,"signature":"` + f.sig + `"`
		}
		entries = append(entries, entry+"}")
	}
	require.NoError(t, os.WriteFile(filepath.Join(dir, "SHA256SUMS"), []byte("sums"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "SHA256SUMS.minisig"), []byte("sig"), 0o600))
	manifest := `{"version":"1.4.0","released_at":"2026-10-26","release_notes_url":"https://example.org/asc/releases/1.4.0",` +
		`"files":[` + strings.Join(entries, ",") + `],"checksums":"SHA256SUMS","checksums_signature":"SHA256SUMS.minisig",` +
		`"minisign_public_key":"RWQf6LRCGA9i53mlYecO4IzT51TGPpvWucNSCh1CBM0QTaLn73Y7GFO3",` +
		`"extensions":{"vscode":"https://marketplace.example/asc","jetbrains":"https://plugins.example/asc"},` +
		`"package_managers":{"winget":"asc.asc","brew":"asc/tap/asc"}}`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "manifest.json"), []byte(manifest), 0o600))
}
