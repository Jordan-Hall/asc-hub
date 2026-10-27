// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package integration

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"forgejo.org/modules/ascdownload"
	"forgejo.org/modules/setting"
	"forgejo.org/tests"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	uaWindows = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 Chrome/130.0 Safari/537.36"
	uaLinux   = "Mozilla/5.0 (X11; Linux x86_64; rv:130.0) Gecko/20100101 Firefox/130.0"
	uaPhone   = "Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 Mobile Safari"
)

// useAscRelease points the hub at a fresh release directory for one test.
func useAscRelease(t *testing.T, base string) string {
	t.Helper()
	dir := t.TempDir()
	writeAscRelease(t, dir)
	oldDir, oldBase := setting.Asc.DownloadDir, setting.Asc.DownloadBaseURL
	setting.Asc.DownloadDir, setting.Asc.DownloadBaseURL = dir, base
	ascdownload.ResetCache()
	t.Cleanup(func() {
		setting.Asc.DownloadDir, setting.Asc.DownloadBaseURL = oldDir, oldBase
		ascdownload.ResetCache()
	})
	return dir
}

func TestAscDownloadHiddenWithoutRelease(t *testing.T) {
	defer tests.PrepareTestEnv(t)()
	old := setting.Asc.DownloadDir
	setting.Asc.DownloadDir = ""
	ascdownload.ResetCache()
	defer func() { setting.Asc.DownloadDir = old; ascdownload.ResetCache() }()

	// a visitor: a page that says there is no release and how to build asc, no link, no section, no file
	visitor := NewHTMLParser(t, MakeRequest(t, NewRequest(t, "GET", "/download"), http.StatusOK).Body)
	assert.Equal(t, 1, visitor.doc.Find(`[data-testid="asc-no-release"]`).Length(), "the page says no release has been published")
	assert.Contains(t, visitor.doc.Find(`[data-testid="asc-no-release"]`).Text(), "cargo build --release -p asc")
	assert.Zero(t, visitor.doc.Find(".ui.warning.message").Length(), "an ordinary visitor sees no setting advice")
	assert.Zero(t, visitor.doc.Find("a.hub-dl-button").Length(), "no download button")
	MakeRequest(t, NewRequest(t, "GET", "/-/download/manifest.json"), http.StatusNotFound)
	home := NewHTMLParser(t, MakeRequest(t, NewRequest(t, "GET", "/"), http.StatusOK).Body)
	assert.Zero(t, home.doc.Find("#download").Length(), "the landing page hides the download section")
	assert.Zero(t, home.doc.Find(`#navbar a[href="/download"]`).Length(), "the header has no download link")

	// an administrator is told what to set
	admin := loginUser(t, "user1")
	doc := NewHTMLParser(t, admin.MakeRequest(t, NewRequest(t, "GET", "/download"), http.StatusOK).Body)
	note := doc.doc.Find(".ui.warning.message").Text()
	assert.Contains(t, note, "DOWNLOAD_DIR")

	// an ordinary user is not
	user := loginUser(t, "user2")
	ordinary := NewHTMLParser(t, user.MakeRequest(t, NewRequest(t, "GET", "/download"), http.StatusOK).Body)
	assert.Zero(t, ordinary.doc.Find(".ui.warning.message").Length())
	assert.Equal(t, 1, ordinary.doc.Find(`[data-testid="asc-no-release"]`).Length())
}

func TestAscDownloadPage(t *testing.T) {
	defer tests.PrepareTestEnv(t)()
	useAscRelease(t, "")

	resp := MakeRequest(t, NewRequest(t, "GET", "/download").SetHeader("User-Agent", uaWindows), http.StatusOK)
	body := resp.Body.String() // before the parser, which drains the buffer
	doc := NewHTMLParser(t, resp.Body)
	root := doc.doc.Find("[data-asc-download]")
	require.Equal(t, 1, root.Length())
	assert.Equal(t, "windows", root.AttrOr("data-active-os", ""))
	assert.Equal(t, "x86_64", root.AttrOr("data-active-arch", ""))
	assert.Equal(t, 3, doc.doc.Find(".hub-dl-panel").Length(), "every system has a panel, so the page works without script")
	assert.Equal(t, 3, doc.doc.Find(`[role="tab"]`).Length())

	assert.Contains(t, body, "Version 1.4.0")
	assert.Contains(t, body, "asc-1.4.0-x86_64-pc-windows-msvc.msi")
	assert.Contains(t, body, setting.AppURL+"-/download/asc-1.4.0-x86_64-pc-windows-msvc.msi", "links point at this hub's own mount")
	assert.Contains(t, body, "asc auth login --host "+strings.TrimSuffix(setting.AppURL, "/")+" --prompt", "the connect step names this hub")
	assert.Contains(t, body, "Get-FileHash")
	assert.Contains(t, body, "winget install --exact --id asc.asc")
	assert.Contains(t, body, "https://marketplace.example/asc")
	assert.Contains(t, body, "https://plugins.example/asc")
	assert.Contains(t, body, "minisign -Vm SHA256SUMS")
	assert.Contains(t, body, "https://example.org/asc/releases/1.4.0")
	assert.NotContains(t, strings.ToLower(doc.doc.Find("main, [role=main]").Text()), "forgejo")

	// the linux visitor gets the linux tab and the install script flow
	doc = NewHTMLParser(t, MakeRequest(t, NewRequest(t, "GET", "/download").SetHeader("User-Agent", uaLinux), http.StatusOK).Body)
	assert.Equal(t, "linux", doc.doc.Find("[data-asc-download]").AttrOr("data-active-os", ""))
	assert.Contains(t, doc.doc.Find("#asc-dl-linux").Text(), "sha256sum -c -")

	// a phone gets no system picked, and still sees every download
	doc = NewHTMLParser(t, MakeRequest(t, NewRequest(t, "GET", "/download").SetHeader("User-Agent", uaPhone), http.StatusOK).Body)
	assert.Equal(t, "windows", doc.doc.Find("[data-asc-download]").AttrOr("data-active-os", ""), "first system of the release")
	assert.Zero(t, doc.doc.Find(".hub-dl-detected").Length(), "no claim about the phone's system")
	assert.Equal(t, 3, doc.doc.Find(".hub-dl-panel").Length())
}

func TestAscDownloadLandingAndHeader(t *testing.T) {
	defer tests.PrepareTestEnv(t)()
	useAscRelease(t, "")

	resp := MakeRequest(t, NewRequest(t, "GET", "/").SetHeader("User-Agent", uaLinux), http.StatusOK)
	doc := NewHTMLParser(t, resp.Body)
	section := doc.doc.Find("#download")
	require.Equal(t, 1, section.Length())
	assert.Contains(t, section.Text(), "Download for Linux")
	assert.Equal(t, 1, doc.doc.Find(`#navbar a[href="/download"]`).Length())

	// the page never shows made up content
	body := strings.ToLower(resp.Body.String())
	for _, banned := range []string{"acme/payments", "hub.example.com", "example.com/acme"} {
		assert.NotContains(t, body, banned)
	}
}

func TestAscDownloadExternalBase(t *testing.T) {
	defer tests.PrepareTestEnv(t)()
	useAscRelease(t, "https://dl.example.org/asc/")

	body := MakeRequest(t, NewRequest(t, "GET", "/download").SetHeader("User-Agent", uaWindows), http.StatusOK).Body.String()
	assert.Contains(t, body, "https://dl.example.org/asc/asc-1.4.0-x86_64-pc-windows-msvc.msi")
	assert.NotContains(t, body, setting.AppURL+"-/download/asc-1.4.0")
}

func TestAscDownloadFiles(t *testing.T) {
	defer tests.PrepareTestEnv(t)()
	dir := useAscRelease(t, "")

	// the manifest is JSON and shown inline; a release file is an attachment that is never sniffed
	resp := MakeRequest(t, NewRequest(t, "GET", "/-/download/manifest.json"), http.StatusOK)
	assert.Contains(t, resp.Header().Get("Content-Type"), "application/json")
	assert.Equal(t, "nosniff", resp.Header().Get("X-Content-Type-Options"))

	name := "asc-1.4.0-x86_64-unknown-linux-gnu.tar.gz"
	resp = MakeRequest(t, NewRequest(t, "GET", "/-/download/"+name), http.StatusOK)
	assert.Equal(t, "asc test file "+name, resp.Body.String())
	assert.Contains(t, resp.Header().Get("Content-Disposition"), "attachment")
	assert.Equal(t, "application/octet-stream", resp.Header().Get("Content-Type"))
	MakeRequest(t, NewRequest(t, "HEAD", "/-/download/"+name), http.StatusOK)
	MakeRequest(t, NewRequest(t, "POST", "/-/download/"+name), http.StatusMethodNotAllowed)

	// listed signatures and checksum files are served
	MakeRequest(t, NewRequest(t, "GET", "/-/download/"+name+".minisig"), http.StatusOK)
	MakeRequest(t, NewRequest(t, "GET", "/-/download/SHA256SUMS"), http.StatusOK)

	// nothing else in the directory is
	require.NoError(t, os.WriteFile(filepath.Join(dir, "secret.txt"), []byte("not published"), 0o600))
	MakeRequest(t, NewRequest(t, "GET", "/-/download/secret.txt"), http.StatusNotFound)
	for _, p := range []string{"..%2f..%2ftests%2fsqlite.ini", "%2e%2e/%2e%2e/go.mod", "sub/../secret.txt", "asc%00.msi", ".."} {
		MakeRequest(t, NewRequest(t, "GET", "/-/download/"+p), http.StatusNotFound)
	}
	MakeRequest(t, NewRequest(t, "GET", "/-/download/"), http.StatusNotFound)
}

func TestAscDownloadSymlinkEscape(t *testing.T) {
	defer tests.PrepareTestEnv(t)()
	dir := useAscRelease(t, "")
	outside := filepath.Join(t.TempDir(), "outside")
	require.NoError(t, os.WriteFile(outside, []byte("outside"), 0o600))
	link := filepath.Join(dir, "asc-1.4.0-x86_64-unknown-linux-gnu.tar.gz.minisig")
	require.NoError(t, os.Remove(link))
	if err := os.Symlink(outside, link); err != nil {
		t.Skip("no symlinks here")
	}
	// a listed name that links out of the directory is not followed
	MakeRequest(t, NewRequest(t, "GET", "/-/download/asc-1.4.0-x86_64-unknown-linux-gnu.tar.gz.minisig"), http.StatusNotFound)
}

func TestAscDownloadBrokenManifestFailsClosed(t *testing.T) {
	defer tests.PrepareTestEnv(t)()
	dir := useAscRelease(t, "")
	manifest := filepath.Join(dir, "manifest.json")
	data, err := os.ReadFile(manifest)
	require.NoError(t, err)

	// a file that is no longer what the manifest says hides the whole release
	require.NoError(t, os.WriteFile(filepath.Join(dir, "install.sh"), []byte("changed after the manifest"), 0o600))
	ascdownload.ResetCache()
	assertNoReleasePage(t)
	MakeRequest(t, NewRequest(t, "GET", "/-/download/install.sh"), http.StatusNotFound)
	home := NewHTMLParser(t, MakeRequest(t, NewRequest(t, "GET", "/"), http.StatusOK).Body)
	assert.Zero(t, home.doc.Find("#download").Length())

	admin := loginUser(t, "user1")
	doc := NewHTMLParser(t, admin.MakeRequest(t, NewRequest(t, "GET", "/download"), http.StatusOK).Body)
	assert.Contains(t, doc.doc.Find(".ui.warning.message").Text(), "install.sh")

	// a manifest with a traversal name is refused
	bad := strings.Replace(string(data), `"name":"install.sh"`, `"name":"../install.sh"`, 1)
	require.NoError(t, os.WriteFile(manifest, []byte(bad), 0o600))
	ascdownload.ResetCache()
	assertNoReleasePage(t)
	// a manifest that is not JSON is refused
	require.NoError(t, os.WriteFile(manifest, []byte("<html>"), 0o600))
	ascdownload.ResetCache()
	assertNoReleasePage(t)
}

// assertNoReleasePage checks that /download offers nothing to download and says why, in the words for a visitor.
func assertNoReleasePage(t *testing.T) {
	t.Helper()
	page := NewHTMLParser(t, MakeRequest(t, NewRequest(t, "GET", "/download"), http.StatusOK).Body)
	assert.Equal(t, 1, page.doc.Find(`[data-testid="asc-no-release"]`).Length())
	assert.Zero(t, page.doc.Find("a.hub-dl-button").Length(), "a release that fails its checks offers no download")
	assert.Zero(t, page.doc.Find(".ui.warning.message").Length())
}
