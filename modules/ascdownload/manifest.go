// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

// Package ascdownload reads the release manifest of the asc command line that an ASC Hub offers on its
// download page, validates it fail closed, and turns it into what the page shows: the right file for a
// visitor's system, the checksum and signature of each file and install commands that verify a download.
//
// The manifest lives in [asc] DOWNLOAD_DIR as manifest.json and is written by the release process, not by a
// visitor, but it is still checked as untrusted input: every name, URL and command word is validated
// before it can reach a link or a command line.
package ascdownload

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// ManifestName is the file name of the manifest inside the download directory.
const ManifestName = "manifest.json"

const (
	maxManifestBytes = 1 << 20
	maxFiles         = 64
	maxFileBytes     = int64(8) << 30
)

// Operating systems, architectures and file kinds a manifest may name.
const (
	OSWindows = "windows"
	OSMacOS   = "macos"
	OSLinux   = "linux"

	ArchX86_64 = "x86_64"
	ArchArm64  = "arm64"
	// ArchAny marks a file that does not depend on the processor, such as an install script.
	ArchAny = "any"

	KindInstaller = "installer" // an msi or pkg that installs asc
	KindArchive   = "archive"   // a zip or tar.gz that holds the asc executable
	KindScript    = "script"    // an install script that takes the archive
)

// File is one downloadable file of a release.
type File struct {
	Name   string `json:"name"`
	OS     string `json:"os"`
	Arch   string `json:"arch"`
	Kind   string `json:"kind"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
	// Signature is the name of the detached minisign signature file of this file, if it has one.
	Signature string `json:"signature,omitempty"`
}

// Extensions are the editor extensions that go with the command line.
type Extensions struct {
	VSCode    string `json:"vscode,omitempty"`
	JetBrains string `json:"jetbrains,omitempty"`
}

// Manifest is manifest.json.
type Manifest struct {
	Version         string `json:"version"`
	ReleasedAt      string `json:"released_at,omitempty"`
	ReleaseNotesURL string `json:"release_notes_url,omitempty"`
	Files           []File `json:"files"`
	// Checksums is the name of a SHA256SUMS style file and ChecksumsSignature the name of its signature.
	Checksums          string `json:"checksums,omitempty"`
	ChecksumsSignature string `json:"checksums_signature,omitempty"`
	// MinisignPublicKey is the public key the signatures verify with (the text of the .pub file's key line).
	MinisignPublicKey string     `json:"minisign_public_key,omitempty"`
	Extensions        Extensions `json:"extensions,omitempty"`
	// PackageManagers maps winget, scoop and brew to the package a command installs, when it is published.
	PackageManagers map[string]string `json:"package_managers,omitempty"`
}

var (
	versionRe   = regexp.MustCompile(`^[0-9]{1,6}\.[0-9]{1,6}\.[0-9]{1,6}(-[0-9A-Za-z][0-9A-Za-z.-]{0,30})?$`)
	fileNameRe  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+-]{0,127}$`)
	sha256Re    = regexp.MustCompile(`^[0-9a-f]{64}$`)
	publicKeyRe = regexp.MustCompile(`^[A-Za-z0-9+/=]{20,120}$`)
	packageRe   = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/+-]{0,99}$`)
	dateRe      = regexp.MustCompile(`^[0-9]{4}-[0-9]{2}-[0-9]{2}$`)
)

// Errors that tell the operator what is missing. None of them carries a visitor's input.
var (
	// ErrNotConfigured means [asc] DOWNLOAD_DIR is empty.
	ErrNotConfigured = errors.New("[asc] DOWNLOAD_DIR is not set")
	// ErrNoManifest means the directory has no manifest.json.
	ErrNoManifest = errors.New("the download directory has no manifest.json")
)

// IsNoRelease reports whether err only means that no release is published (as opposed to a broken one).
func IsNoRelease(err error) bool {
	return errors.Is(err, ErrNotConfigured) || errors.Is(err, ErrNoManifest)
}

// validName reports whether name is one plain, safe file name.
func validName(name string) bool {
	return fileNameRe.MatchString(name) && !strings.Contains(name, "..")
}

// validHTTPSURL reports whether raw is an absolute https URL without credentials or a fragment. The hub
// never fetches these; they are only rendered as links.
func validHTTPSURL(raw string) bool {
	if len(raw) > 512 {
		return false
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.Fragment != "" {
		return false
	}
	for _, r := range raw {
		if r <= ' ' || r == 0x7f {
			return false
		}
	}
	return true
}

// Validate checks a manifest as untrusted data. It does not look at the disk.
func (m *Manifest) Validate() error {
	if !versionRe.MatchString(m.Version) {
		return fmt.Errorf("version %q is not a version number like 1.2.3", clip(m.Version))
	}
	if m.ReleasedAt != "" && !dateRe.MatchString(m.ReleasedAt) {
		return errors.New("released_at must be a date like 2026-10-26")
	}
	if m.ReleaseNotesURL != "" && !validHTTPSURL(m.ReleaseNotesURL) {
		return errors.New("release_notes_url must be an https URL")
	}
	for name, u := range map[string]string{"extensions.vscode": m.Extensions.VSCode, "extensions.jetbrains": m.Extensions.JetBrains} {
		if u != "" && !validHTTPSURL(u) {
			return fmt.Errorf("%s must be an https URL", name)
		}
	}
	if m.MinisignPublicKey != "" && !publicKeyRe.MatchString(m.MinisignPublicKey) {
		return errors.New("minisign_public_key is not a minisign public key")
	}
	for key, val := range m.PackageManagers {
		switch key {
		case "winget", "scoop", "brew":
		default:
			return fmt.Errorf("package_managers has an unknown manager %q", clip(key))
		}
		if !packageRe.MatchString(val) {
			return fmt.Errorf("package_managers.%s is not a package name", key)
		}
	}
	if len(m.Files) == 0 {
		return errors.New("files is empty")
	}
	if len(m.Files) > maxFiles {
		return fmt.Errorf("files lists more than %d files", maxFiles)
	}
	seen := map[string]bool{}
	for _, f := range m.Files {
		if !validName(f.Name) {
			return fmt.Errorf("file name %q is not a plain file name", clip(f.Name))
		}
		if seen[f.Name] {
			return fmt.Errorf("file %s is listed twice", f.Name)
		}
		seen[f.Name] = true
		switch f.OS {
		case OSWindows, OSMacOS, OSLinux:
		default:
			return fmt.Errorf("file %s has os %q; use windows, macos or linux", f.Name, clip(f.OS))
		}
		switch f.Arch {
		case ArchX86_64, ArchArm64, ArchAny:
		default:
			return fmt.Errorf("file %s has arch %q; use x86_64, arm64 or any", f.Name, clip(f.Arch))
		}
		switch f.Kind {
		case KindInstaller, KindArchive, KindScript:
		default:
			return fmt.Errorf("file %s has kind %q; use installer, archive or script", f.Name, clip(f.Kind))
		}
		if !sha256Re.MatchString(f.SHA256) {
			return fmt.Errorf("file %s needs a lower case hex sha256", f.Name)
		}
		if f.Size <= 0 || f.Size > maxFileBytes {
			return fmt.Errorf("file %s has a size outside 1 byte to 8 GiB", f.Name)
		}
		if f.Signature != "" && !validName(f.Signature) {
			return fmt.Errorf("signature of %s is not a plain file name", f.Name)
		}
	}
	for _, n := range []string{m.Checksums, m.ChecksumsSignature} {
		if n != "" && !validName(n) {
			return errors.New("checksums and checksums_signature must be plain file names")
		}
	}
	if m.ChecksumsSignature != "" && m.Checksums == "" {
		return errors.New("checksums_signature needs checksums")
	}
	for _, n := range m.extraNames() {
		// a signature or checksum file must not also be a listed download: it would show twice
		if seen[n] {
			return fmt.Errorf("%s is listed as a download and also as a signature or checksum file", n)
		}
	}
	return nil
}

func (m *Manifest) extraNames() []string {
	var out []string
	for _, f := range m.Files {
		if f.Signature != "" {
			out = append(out, f.Signature)
		}
	}
	if m.Checksums != "" {
		out = append(out, m.Checksums)
	}
	if m.ChecksumsSignature != "" {
		out = append(out, m.ChecksumsSignature)
	}
	return out
}

// ServedNames is the set of file names a hub may serve from the download directory: the manifest, every
// listed file, every signature and the checksum files. Nothing else in the directory is reachable.
func (m *Manifest) ServedNames() map[string]bool {
	out := map[string]bool{ManifestName: true}
	for _, f := range m.Files {
		out[f.Name] = true
	}
	for _, n := range m.extraNames() {
		out[n] = true
	}
	return out
}

// Find returns the listed file called name.
func (m *Manifest) Find(name string) (File, bool) {
	for _, f := range m.Files {
		if f.Name == name {
			return f, true
		}
	}
	return File{}, false
}

func clip(s string) string {
	s = strings.Map(func(r rune) rune {
		if r < ' ' || r == 0x7f {
			return '?'
		}
		return r
	}, s)
	if len(s) > 40 {
		return s[:40] + "..."
	}
	return s
}

// Parse reads and validates a manifest from r, refusing more than 1 MiB.
func Parse(r io.Reader) (*Manifest, error) {
	data, err := io.ReadAll(io.LimitReader(r, maxManifestBytes+1))
	if err != nil {
		return nil, fmt.Errorf("reading manifest.json: %w", err)
	}
	if len(data) > maxManifestBytes {
		return nil, errors.New("manifest.json is larger than 1 MiB")
	}
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("manifest.json is not valid: %w", jsonErr(err))
	}
	if err := m.Validate(); err != nil {
		return nil, fmt.Errorf("manifest.json: %w", err)
	}
	sort.SliceStable(m.Files, func(i, j int) bool { return kindRank(m.Files[i].Kind) < kindRank(m.Files[j].Kind) })
	return &m, nil
}

// jsonErr drops the offending value from a decoder error, which could be long.
func jsonErr(err error) error {
	var te *json.UnmarshalTypeError
	if errors.As(err, &te) {
		return fmt.Errorf("field %s has the wrong type", te.Field)
	}
	var se *json.SyntaxError
	if errors.As(err, &se) {
		return fmt.Errorf("syntax error at byte %d", se.Offset)
	}
	return errors.New("unreadable JSON")
}

func kindRank(kind string) int {
	switch kind {
	case KindInstaller:
		return 0
	case KindArchive:
		return 1
	}
	return 2
}

// Load reads the manifest of dir. A local download mount also needs every listed file to exist with the
// listed size (checkFiles); with an external base URL the files are not on this disk.
func Load(dir string, checkFiles bool) (*Manifest, error) {
	if dir == "" {
		return nil, ErrNotConfigured
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, ErrNoManifest
		}
		return nil, errors.New("the download directory cannot be opened")
	}
	defer root.Close()
	f, err := root.Open(ManifestName)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, ErrNoManifest
		}
		return nil, errors.New("manifest.json cannot be opened")
	}
	defer f.Close()
	if st, err := f.Stat(); err != nil || !st.Mode().IsRegular() {
		return nil, errors.New("manifest.json is not a regular file")
	}
	m, err := Parse(f)
	if err != nil {
		return nil, err
	}
	if checkFiles {
		for _, f := range m.Files {
			st, err := root.Stat(f.Name)
			if err != nil || !st.Mode().IsRegular() {
				return nil, fmt.Errorf("listed file %s is missing from the download directory", f.Name)
			}
			if st.Size() != f.Size {
				return nil, fmt.Errorf("listed file %s has a different size than the manifest says", f.Name)
			}
		}
	}
	return m, nil
}

type cacheEntry struct {
	dir     string
	check   bool
	mtime   time.Time
	size    int64
	checked time.Time
	m       *Manifest
	err     error
}

var (
	cacheMu sync.Mutex
	cache   cacheEntry
)

const cacheTTL = 5 * time.Second

// Cached is Load with a cache: a request within five seconds of the last read, or one that finds the
// manifest unchanged, does not read it again.
func Cached(dir string, checkFiles bool) (*Manifest, error) {
	cacheMu.Lock()
	defer cacheMu.Unlock()
	now := time.Now()
	if cache.dir == dir && cache.check == checkFiles && now.Sub(cache.checked) < cacheTTL {
		return cache.m, cache.err
	}
	var mtime time.Time
	var size int64
	if dir != "" {
		if root, err := os.OpenRoot(dir); err == nil {
			if st, err := root.Stat(ManifestName); err == nil {
				mtime, size = st.ModTime(), st.Size()
			}
			root.Close()
		}
	}
	if cache.dir == dir && cache.check == checkFiles && cache.mtime.Equal(mtime) && cache.size == size && !mtime.IsZero() && cache.err == nil {
		cache.checked = now
		return cache.m, cache.err
	}
	m, err := Load(dir, checkFiles)
	cache = cacheEntry{dir: dir, check: checkFiles, mtime: mtime, size: size, checked: now, m: m, err: err}
	return m, err
}

// ResetCache forgets the cached manifest (for tests).
func ResetCache() {
	cacheMu.Lock()
	cache = cacheEntry{}
	cacheMu.Unlock()
}
