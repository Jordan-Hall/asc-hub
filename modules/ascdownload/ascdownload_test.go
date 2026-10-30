// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package ascdownload

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const hexA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
const hexB = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"

func sample() Manifest {
	return Manifest{
		Version:         "1.2.3",
		ReleasedAt:      "2026-10-26",
		ReleaseNotesURL: "https://example.org/notes",
		Files: []File{
			{Name: "asc-1.2.3-x86_64-unknown-linux-gnu.tar.gz", OS: OSLinux, Arch: ArchX86_64, Kind: KindArchive, SHA256: hexA, Size: 100, Signature: "asc-1.2.3-x86_64-unknown-linux-gnu.tar.gz.minisig"},
			{Name: "install.sh", OS: OSLinux, Arch: ArchAny, Kind: KindScript, SHA256: hexB, Size: 10},
			{Name: "asc-1.2.3-x86_64-pc-windows-msvc.msi", OS: OSWindows, Arch: ArchX86_64, Kind: KindInstaller, SHA256: hexB, Size: 200},
			{Name: "asc-1.2.3-aarch64-apple-darwin.tar.gz", OS: OSMacOS, Arch: ArchArm64, Kind: KindArchive, SHA256: hexA, Size: 300},
		},
		Checksums:          "SHA256SUMS",
		ChecksumsSignature: "SHA256SUMS.minisig",
		MinisignPublicKey:  "RWQf6LRCGA9i53mlYecO4IzT51TGPpvWucNSCh1CBM0QTaLn73Y7GFO3",
		Extensions:         Extensions{VSCode: "https://marketplace.example/asc", JetBrains: "https://plugins.example/asc"},
		PackageManagers:    map[string]string{"winget": "asc.asc", "brew": "asc/tap/asc"},
	}
}

func TestValidateAccepts(t *testing.T) {
	m := sample()
	require.NoError(t, m.Validate())
	assert.Equal(t, map[string]bool{
		"manifest.json": true, "install.sh": true, "SHA256SUMS": true, "SHA256SUMS.minisig": true,
		"asc-1.2.3-x86_64-unknown-linux-gnu.tar.gz":         true,
		"asc-1.2.3-x86_64-unknown-linux-gnu.tar.gz.minisig": true,
		"asc-1.2.3-x86_64-pc-windows-msvc.msi":              true,
		"asc-1.2.3-aarch64-apple-darwin.tar.gz":             true,
	}, m.ServedNames())
}

func TestValidateRejects(t *testing.T) {
	for name, mutate := range map[string]func(*Manifest){
		"version":             func(m *Manifest) { m.Version = "latest" },
		"version injection":   func(m *Manifest) { m.Version = "1.0.0\"><script>" },
		"date":                func(m *Manifest) { m.ReleasedAt = "yesterday" },
		"notes http":          func(m *Manifest) { m.ReleaseNotesURL = "http://example.org" },
		"notes javascript":    func(m *Manifest) { m.ReleaseNotesURL = "javascript:alert(1)" },
		"notes credentials":   func(m *Manifest) { m.ReleaseNotesURL = "https://u:p@example.org" },
		"notes space":         func(m *Manifest) { m.ReleaseNotesURL = "https://example.org/a b" },
		"vscode":              func(m *Manifest) { m.Extensions.VSCode = "vscode:extension/x" },
		"key":                 func(m *Manifest) { m.MinisignPublicKey = "RW' ; rm -rf /" },
		"manager":             func(m *Manifest) { m.PackageManagers["apt"] = "asc" },
		"package injection":   func(m *Manifest) { m.PackageManagers["brew"] = "asc; rm -rf ~" },
		"no files":            func(m *Manifest) { m.Files = nil },
		"path in name":        func(m *Manifest) { m.Files[0].Name = "../asc.tar.gz" },
		"slash in name":       func(m *Manifest) { m.Files[0].Name = "a/b.tar.gz" },
		"backslash in name":   func(m *Manifest) { m.Files[0].Name = `a\b.tar.gz` },
		"dotdot in name":      func(m *Manifest) { m.Files[0].Name = "a..b.tar.gz" },
		"leading dot name":    func(m *Manifest) { m.Files[0].Name = ".hidden" },
		"quote in name":       func(m *Manifest) { m.Files[0].Name = "a'b.tar.gz" },
		"duplicate":           func(m *Manifest) { m.Files[1].Name = m.Files[0].Name },
		"os":                  func(m *Manifest) { m.Files[0].OS = "plan9" },
		"arch":                func(m *Manifest) { m.Files[0].Arch = "riscv" },
		"kind":                func(m *Manifest) { m.Files[0].Kind = "exe" },
		"sha upper":           func(m *Manifest) { m.Files[0].SHA256 = strings.ToUpper(hexA) },
		"sha short":           func(m *Manifest) { m.Files[0].SHA256 = "abc" },
		"size zero":           func(m *Manifest) { m.Files[0].Size = 0 },
		"size huge":           func(m *Manifest) { m.Files[0].Size = int64(9) << 30 },
		"signature path":      func(m *Manifest) { m.Files[0].Signature = "../x.minisig" },
		"checksums path":      func(m *Manifest) { m.Checksums = "../SHA256SUMS" },
		"sig without sums":    func(m *Manifest) { m.Checksums = "" },
		"signature is a file": func(m *Manifest) { m.Files[0].Signature = "install.sh" },
	} {
		m := sample()
		m.Files = append([]File(nil), m.Files...)
		pm := map[string]string{}
		for k, v := range m.PackageManagers {
			pm[k] = v
		}
		m.PackageManagers = pm
		mutate(&m)
		assert.Error(t, m.Validate(), name)
	}
	m := sample()
	for i := 0; i <= maxFiles; i++ {
		m.Files = append(m.Files, File{Name: "f" + strings.Repeat("a", i%50) + string(rune('a'+i%26)) + string(rune('a'+i/26)) + ".zip", OS: OSLinux, Arch: ArchX86_64, Kind: KindArchive, SHA256: hexA, Size: 1})
	}
	assert.Error(t, m.Validate(), "too many files")
}

func TestParse(t *testing.T) {
	data, err := json.Marshal(sample())
	require.NoError(t, err)
	m, err := Parse(strings.NewReader(string(data)))
	require.NoError(t, err)
	assert.Equal(t, "1.2.3", m.Version)
	assert.Equal(t, KindInstaller, m.Files[0].Kind, "installers sort before archives and scripts")

	_, err = Parse(strings.NewReader("{"))
	require.Error(t, err)
	_, err = Parse(strings.NewReader(`{"version":3}`))
	require.Error(t, err)
	_, err = Parse(strings.NewReader(strings.Repeat(" ", maxManifestBytes+1)))
	require.ErrorContains(t, err, "larger than 1 MiB")
}

func writeRelease(t *testing.T, dir string, m Manifest) {
	t.Helper()
	data, err := json.Marshal(m)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, ManifestName), data, 0o600))
	for _, f := range m.Files {
		require.NoError(t, os.WriteFile(filepath.Join(dir, f.Name), make([]byte, f.Size), 0o600))
	}
}

func TestLoad(t *testing.T) {
	_, err := Load("", true)
	require.ErrorIs(t, err, ErrNotConfigured)
	assert.True(t, IsNoRelease(err))

	dir := t.TempDir()
	_, err = Load(dir, true)
	require.ErrorIs(t, err, ErrNoManifest)
	_, err = Load(filepath.Join(dir, "missing"), true)
	require.ErrorIs(t, err, ErrNoManifest)

	m := sample()
	writeRelease(t, dir, m)
	got, err := Load(dir, true)
	require.NoError(t, err)
	assert.Equal(t, "1.2.3", got.Version)

	// a listed file with a different size fails closed
	require.NoError(t, os.WriteFile(filepath.Join(dir, "install.sh"), []byte("short but not 10 b"), 0o600))
	_, err = Load(dir, true)
	require.ErrorContains(t, err, "different size")
	_, err = Load(dir, false)
	require.NoError(t, err, "an external base URL does not check the disk")

	// a missing file fails closed
	require.NoError(t, os.Remove(filepath.Join(dir, "install.sh")))
	_, err = Load(dir, true)
	require.ErrorContains(t, err, "missing")
	assert.False(t, IsNoRelease(err))
}

func TestLoadRefusesSymlinkedManifest(t *testing.T) {
	dir := t.TempDir()
	outside := t.TempDir()
	data, _ := json.Marshal(sample())
	require.NoError(t, os.WriteFile(filepath.Join(outside, "m.json"), data, 0o600))
	if err := os.Symlink(filepath.Join(outside, "m.json"), filepath.Join(dir, ManifestName)); err != nil {
		t.Skip("no symlinks here")
	}
	_, err := Load(dir, false)
	require.Error(t, err, "a manifest that links out of the directory is not read")
}

func TestCachedFollowsChanges(t *testing.T) {
	ResetCache()
	t.Cleanup(ResetCache)
	dir := t.TempDir()
	m := sample()
	writeRelease(t, dir, m)
	got, err := Cached(dir, true)
	require.NoError(t, err)
	assert.Equal(t, "1.2.3", got.Version)

	m.Version = "1.2.4"
	writeRelease(t, dir, m)
	ResetCache() // the five second window would otherwise serve the old read
	got, err = Cached(dir, true)
	require.NoError(t, err)
	assert.Equal(t, "1.2.4", got.Version)
}

func TestDetectPlatform(t *testing.T) {
	for ua, want := range map[string]Platform{
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64) Chrome/130":                 {OSWindows, ArchX86_64},
		"Mozilla/5.0 (Windows NT 10.0; ARM64) Edge":                            {OSWindows, ArchArm64},
		"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) Safari":               {OSMacOS, ArchArm64},
		"Mozilla/5.0 (X11; Linux x86_64) Firefox":                              {OSLinux, ArchX86_64},
		"Mozilla/5.0 (X11; Linux aarch64) Firefox":                             {OSLinux, ArchArm64},
		"Mozilla/5.0 (Linux; Android 14; Pixel) Chrome Mobile":                 {},
		"Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) Safari Mobile": {},
		"Mozilla/5.0 (X11; CrOS x86_64 14541.0.0) Chrome":                      {},
		"curl/8.0": {},
		"":         {},
	} {
		assert.Equal(t, want, DetectPlatform(ua), ua)
	}
}

func TestBuildOffersAndPick(t *testing.T) {
	m := sample()
	o := BuildOffers(&m, "https://hub.example.com/-/download/", "https://hub.example.com/")
	require.Len(t, o.OSes, 3)
	assert.Equal(t, []string{OSMacOS, OSLinux, OSWindows}, []string{o.OSes[0].OS, o.OSes[1].OS, o.OSes[2].OS}, "the order of the boards")

	os, arch, ok := o.Pick(Platform{OSLinux, ArchX86_64})
	require.True(t, ok)
	assert.Equal(t, "Linux", os.Label)
	assert.Equal(t, ArchX86_64, arch.Arch)
	require.NotNil(t, arch.Primary)
	assert.Equal(t, "asc-1.2.3-x86_64-unknown-linux-gnu.tar.gz", arch.Primary.Name)
	assert.Equal(t, "https://hub.example.com/-/download/asc-1.2.3-x86_64-unknown-linux-gnu.tar.gz", arch.Primary.URL)
	assert.Len(t, arch.Files, 2, "the install script is listed with the build")
	require.Len(t, arch.Commands, 1)
	cmd := arch.Commands[0].Text
	assert.Contains(t, cmd, "sha256sum -c -")
	assert.Contains(t, cmd, "sh install.sh --tarball asc-1.2.3-x86_64-unknown-linux-gnu.tar.gz --sig asc-1.2.3-x86_64-unknown-linux-gnu.tar.gz.minisig")
	assert.Less(t, strings.Index(cmd, "sha256sum"), strings.Index(cmd, "sh install.sh"), "checksum first")

	// no arm64 Linux build: fall back to what exists for the operating system
	_, arch, ok = o.Pick(Platform{OSLinux, ArchArm64})
	require.True(t, ok)
	assert.Equal(t, ArchX86_64, arch.Arch)

	_, _, ok = o.Pick(Platform{})
	assert.False(t, ok)

	win, arch, _ := o.Pick(Platform{OSWindows, ArchX86_64})
	cmd = arch.Commands[0].Text
	assert.Contains(t, cmd, "Get-FileHash")
	assert.Contains(t, cmd, "'"+strings.ToUpper(hexB)+"'")
	assert.Less(t, strings.Index(cmd, "Get-FileHash"), strings.Index(cmd, "msiexec"), "checksum first")
	require.Len(t, win.PackageCommands, 1)
	assert.Equal(t, "winget install --exact --id asc.asc", win.PackageCommands[0].Text)

	mac, arch, _ := o.Pick(Platform{OSMacOS, ArchArm64})
	assert.Contains(t, arch.Commands[0].Text, "shasum -a 256 -c -")
	require.Len(t, mac.PackageCommands, 1)
	assert.Equal(t, "brew install asc/tap/asc", mac.PackageCommands[0].Text)

	require.NotNil(t, o.Checksums)
	require.NotNil(t, o.ChecksumsSig)
	assert.Equal(t, "minisign -Vm SHA256SUMS -x SHA256SUMS.minisig -P RWQf6LRCGA9i53mlYecO4IzT51TGPpvWucNSCh1CBM0QTaLn73Y7GFO3", o.VerifyCommand)
	assert.Equal(t, "asc auth login --host https://hub.example.com --prompt", o.ConnectCommand)
}

func TestCommandsQuoteTheBase(t *testing.T) {
	m := sample()
	// a base URL may hold a single quote in its path; it must not end the quoted word
	o := BuildOffers(&m, "https://dl.example.com/it's/", "https://hub.example.com/sub path/")
	_, arch, _ := o.Pick(Platform{OSLinux, ArchX86_64})
	assert.Contains(t, arch.Commands[0].Text, `'https://dl.example.com/it'\''s/install.sh'`)
	_, arch, _ = o.Pick(Platform{OSWindows, ArchX86_64})
	assert.Contains(t, arch.Commands[0].Text, `'https://dl.example.com/it''s/asc-1.2.3-x86_64-pc-windows-msvc.msi'`)
	assert.Equal(t, "asc auth login --host 'https://hub.example.com/sub path' --prompt", o.ConnectCommand)
}

// the manifest.json that asc's release process writes (docs/RELEASE.md of asc, scripts/release/assemble.py)
const ascSchema1 = `{
  "schema": 1, "name": "asc", "version": "1.2.3", "commit": "0123456789abcdef0123456789abcdef01234567", "released_at": "2026-10-26T12:00:00Z",
  "files": [
    {"version": "1.2.3", "kind": "cli", "os": "linux", "arch": "x86_64", "target": "x86_64-unknown-linux-gnu", "format": "tar.gz", "file": "asc-1.2.3-x86_64-unknown-linux-gnu.tar.gz", "sha256": "` + hexA + `", "size": 1000, "signature": "asc-1.2.3-x86_64-unknown-linux-gnu.tar.gz.minisig"},
    {"version": "1.2.3", "kind": "cli", "os": "linux", "arch": "x86_64", "target": "x86_64-unknown-linux-gnu", "format": "zip", "file": "asc-1.2.3-x86_64-unknown-linux-gnu.zip", "sha256": "` + hexB + `", "size": 1001, "signature": "asc-1.2.3-x86_64-unknown-linux-gnu.zip.minisig"},
    {"version": "1.2.3", "kind": "cli", "os": "macos", "arch": "aarch64", "target": "aarch64-apple-darwin", "format": "tar.gz", "file": "asc-1.2.3-aarch64-apple-darwin.tar.gz", "sha256": "` + hexA + `", "size": 1002, "signature": "asc-1.2.3-aarch64-apple-darwin.tar.gz.minisig"},
    {"version": "1.2.3", "kind": "cli", "os": "windows", "arch": "x86_64", "target": "x86_64-pc-windows-msvc", "format": "zip", "file": "asc-1.2.3-x86_64-pc-windows-msvc.zip", "sha256": "` + hexB + `", "size": 1003, "signature": "asc-1.2.3-x86_64-pc-windows-msvc.zip.minisig"},
    {"version": "1.2.3", "kind": "vscode", "os": "any", "arch": "any", "target": null, "format": "vsix", "file": "asc-vscode-1.2.3.vsix", "sha256": "` + hexA + `", "size": 10, "signature": "asc-vscode-1.2.3.vsix.minisig"},
    {"version": "1.2.3", "kind": "sbom", "os": "any", "arch": "any", "target": null, "format": "json", "file": "asc-1.2.3.cdx.json", "sha256": "` + hexA + `", "size": 10, "signature": "asc-1.2.3.cdx.json.minisig"},
    {"version": "1.2.3", "kind": "homebrew", "os": "any", "arch": "any", "target": null, "format": "rb", "file": "asc.rb", "sha256": "` + hexA + `", "size": 10, "signature": "asc.rb.minisig"},
    {"version": "1.2.3", "kind": "aur", "os": "any", "arch": "any", "target": null, "format": "pkgbuild", "file": "PKGBUILD", "sha256": "` + hexA + `", "size": 10, "signature": "PKGBUILD.minisig"}
  ]
}`

func TestParseReadsTheManifestOfAscsReleaseProcess(t *testing.T) {
	m, err := Parse(strings.NewReader(ascSchema1))
	require.NoError(t, err)
	assert.Equal(t, "1.2.3", m.Version)
	assert.Equal(t, "2026-10-26", m.ReleasedAt, "the day of the release time")
	require.Len(t, m.Files, 4, "the command line files only: no editor package, SBOM, formula or PKGBUILD")
	byName := map[string]File{}
	for _, f := range m.Files {
		byName[f.Name] = f
	}
	mac := byName["asc-1.2.3-aarch64-apple-darwin.tar.gz"]
	assert.Equal(t, OSMacOS, mac.OS)
	assert.Equal(t, ArchArm64, mac.Arch, "aarch64 is arm64 on the page")
	assert.Equal(t, KindArchive, mac.Kind)
	assert.Equal(t, "asc-1.2.3-aarch64-apple-darwin.tar.gz.minisig", mac.Signature)
	assert.EqualValues(t, 1002, mac.Size)
	assert.Equal(t, KindArchive, byName["asc-1.2.3-x86_64-pc-windows-msvc.zip"].Kind)
	assert.Equal(t, "SHA256SUMS", m.Checksums)
	assert.Equal(t, "SHA256SUMS.minisig", m.ChecksumsSignature)
	assert.Empty(t, m.PackageManagers, "nothing is published to a tap, bucket or index, so the page names no package manager")

	// the offers: a page for each of the three systems
	o := BuildOffers(m, "https://hub.example.com/-/download/", "https://hub.example.com/")
	require.Len(t, o.OSes, 3)
	assert.Empty(t, o.OSes[0].PackageCommands)

	// another schema, and a manifest with nothing to offer, are refused
	_, err = Parse(strings.NewReader(strings.Replace(ascSchema1, `"schema": 1`, `"schema": 2`, 1)))
	require.ErrorContains(t, err, "schema 2")
	_, err = Parse(strings.NewReader(`{"schema": 1, "version": "1.2.3", "files": [{"kind": "sbom", "file": "x.json"}]}`))
	require.ErrorContains(t, err, "no command line files")
	// the values are checked as for any manifest
	_, err = Parse(strings.NewReader(strings.Replace(ascSchema1, `"file": "asc-1.2.3-x86_64-pc-windows-msvc.zip"`, `"file": "../x.zip"`, 1)))
	require.Error(t, err)
}
