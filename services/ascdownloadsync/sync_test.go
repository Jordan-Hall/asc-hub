// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package ascdownloadsync

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"forgejo.org/modules/ascdownload"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/blake2b"
)

const testToken = "ghp_test_token_value"

// A signer makes minisign keys and signatures.
type signer struct {
	id   [8]byte
	priv ed25519.PrivateKey
	pub  ed25519.PublicKey
}

func newSigner(t *testing.T, id byte) *signer {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)
	s := &signer{priv: priv, pub: pub}
	for i := range s.id {
		s.id[i] = id
	}
	return s
}

func (s *signer) publicKey() string {
	raw := append(append([]byte("Ed"), s.id[:]...), s.pub...)
	return base64.StdEncoding.EncodeToString(raw)
}

// sign makes a .minisig of data, prehashed ("ED") or legacy ("Ed").
func (s *signer) sign(data []byte, prehashed bool) string {
	msg, alg := data, "Ed"
	if prehashed {
		d := blake2b.Sum512(data)
		msg, alg = d[:], "ED"
	}
	sig := ed25519.Sign(s.priv, msg)
	comment := "timestamp:1 file:x"
	global := ed25519.Sign(s.priv, append(append([]byte(nil), sig...), []byte(comment)...))
	line := base64.StdEncoding.EncodeToString(append(append([]byte(alg), s.id[:]...), sig...))
	return "untrusted comment: signature from minisign secret key\n" + line + "\ntrusted comment: " + comment + "\n" + base64.StdEncoding.EncodeToString(global) + "\n"
}

// release is a signed release as the source serves it.
type release struct {
	files map[string][]byte
}

func sum(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }

// makeRelease builds version v signed by s, with one archive and one script. tamper names a file whose content is
// changed after the checksums were made.
func makeRelease(t *testing.T, s *signer, version string, opts ...func(*ascdownload.Manifest, map[string][]byte, *signer)) release {
	t.Helper()
	archive := []byte("archive of asc " + version + strings.Repeat("x", 100))
	script := []byte("#!/bin/sh\necho install " + version + "\n")
	archName, scriptName := "asc-"+version+"-linux-x86_64.tar.gz", "install-"+version+".sh"
	files := map[string][]byte{archName: archive, scriptName: script}
	m := ascdownload.Manifest{
		Version: version, ReleasedAt: "2026-10-26",
		Files: []ascdownload.File{
			{Name: archName, OS: "linux", Arch: "x86_64", Kind: "archive", SHA256: sum(archive), Size: int64(len(archive)), Signature: archName + ".minisig"},
			{Name: scriptName, OS: "linux", Arch: "any", Kind: "script", SHA256: sum(script), Size: int64(len(script)), Signature: scriptName + ".minisig"},
		},
		Checksums: "SHA256SUMS", ChecksumsSignature: "SHA256SUMS.minisig", MinisignPublicKey: s.publicKey(),
	}
	files[archName+".minisig"] = []byte(s.sign(archive, true))
	files[scriptName+".minisig"] = []byte(s.sign(script, false)) // a legacy signature
	for _, o := range opts {
		o(&m, files, s)
	}
	mb, err := json.Marshal(m)
	require.NoError(t, err)
	files["manifest.json"] = mb
	var sums strings.Builder
	for _, f := range m.Files {
		fmt.Fprintf(&sums, "%s  %s\n", f.SHA256, f.Name)
	}
	fmt.Fprintf(&sums, "%s  manifest.json\n", sum(mb))
	files["SHA256SUMS"] = []byte(sums.String())
	files["SHA256SUMS.minisig"] = []byte(s.sign(files["SHA256SUMS"], true))
	return release{files: files}
}

// fakeSource serves releases from a TLS server, as a directory or as the API of a release.
type fakeSource struct {
	mu       sync.Mutex
	rel      release
	srv      *httptest.Server
	auth     []string // the Authorization header of every request
	gets     map[string]int
	asRepo   bool
	tamper   func(name string, data []byte) []byte
	oversize bool
}

func newSource(t *testing.T, rel release) *fakeSource {
	t.Helper()
	s := &fakeSource{rel: rel, gets: map[string]int{}}
	s.srv = httptest.NewTLSServer(http.HandlerFunc(s.handle))
	t.Cleanup(s.srv.Close)
	return s
}

func (s *fakeSource) handle(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.auth = append(s.auth, r.Header.Get("Authorization"))
	serve := func(name string) {
		data, ok := s.rel.files[name]
		if !ok {
			http.NotFound(w, r)
			return
		}
		s.gets[name]++
		if s.tamper != nil {
			data = s.tamper(name, data)
		}
		if s.oversize {
			data = append(data, make([]byte, 1000)...)
		}
		_, _ = w.Write(data)
	}
	switch {
	case s.asRepo && r.URL.Path == "/repos/o/r/releases/latest":
		if r.Header.Get("Authorization") != "Bearer "+testToken {
			http.Error(w, "private", http.StatusNotFound)
			return
		}
		type asset struct {
			Name string `json:"name"`
			URL  string `json:"url"`
		}
		var assets []asset
		for name := range s.rel.files {
			assets = append(assets, asset{name, "https://" + r.Host + "/repos/o/r/releases/assets/" + name})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"assets": assets})
	case s.asRepo && strings.HasPrefix(r.URL.Path, "/repos/o/r/releases/assets/"):
		if r.Header.Get("Authorization") != "Bearer "+testToken || r.Header.Get("Accept") != "application/octet-stream" {
			http.Error(w, "private", http.StatusNotFound)
			return
		}
		serve(strings.TrimPrefix(r.URL.Path, "/repos/o/r/releases/assets/"))
	case !s.asRepo && strings.HasPrefix(r.URL.Path, "/rel/"):
		if r.Header.Get("Authorization") != "Bearer "+testToken {
			http.Error(w, "private", http.StatusUnauthorized)
			return
		}
		serve(strings.TrimPrefix(r.URL.Path, "/rel/"))
	default:
		http.NotFound(w, r)
	}
}

func (s *fakeSource) url() string {
	if s.asRepo {
		return s.srv.URL + "/repos/o/r/releases/latest"
	}
	return s.srv.URL + "/rel"
}

func newSyncer(t *testing.T, s *fakeSource, sg *signer) (*Syncer, string) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "downloads")
	sy, err := New(Config{SourceURL: s.url(), Token: testToken, PublicKey: sg.publicKey(), Dir: dir}, s.srv.Client())
	require.NoError(t, err)
	t.Cleanup(ascdownload.ResetCache)
	return sy, dir
}

func published(t *testing.T, dir string) *ascdownload.Manifest {
	t.Helper()
	ascdownload.ResetCache()
	m, err := ascdownload.Load(dir, true)
	require.NoError(t, err)
	return m
}

func names(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	var out []string
	for _, e := range entries {
		out = append(out, e.Name())
	}
	return out
}

func TestSyncPublishesASignedReleaseFromADirectory(t *testing.T) {
	sg := newSigner(t, 1)
	src := newSource(t, makeRelease(t, sg, "1.2.3"))
	sy, dir := newSyncer(t, src, sg)

	res, err := sy.Sync(t.Context())
	require.NoError(t, err)
	assert.Equal(t, "1.2.3", res.Version)
	assert.True(t, res.Changed)
	m := published(t, dir)
	assert.Equal(t, "1.2.3", m.Version)
	got, err := os.ReadFile(filepath.Join(dir, "asc-1.2.3-linux-x86_64.tar.gz"))
	require.NoError(t, err)
	assert.Equal(t, src.rel.files["asc-1.2.3-linux-x86_64.tar.gz"], got)
	assert.ElementsMatch(t, []string{"manifest.json", "SHA256SUMS", "SHA256SUMS.minisig", "asc-1.2.3-linux-x86_64.tar.gz", "asc-1.2.3-linux-x86_64.tar.gz.minisig", "install-1.2.3.sh", "install-1.2.3.sh.minisig"}, names(t, dir), "no staging directory is left")
	for _, a := range src.auth {
		assert.Equal(t, "Bearer "+testToken, a, "every request carries the token")
	}

	// A second run with nothing new downloads no file.
	before := src.gets["asc-1.2.3-linux-x86_64.tar.gz"]
	res, err = sy.Sync(t.Context())
	require.NoError(t, err)
	assert.False(t, res.Changed)
	assert.Equal(t, before, src.gets["asc-1.2.3-linux-x86_64.tar.gz"])
	last, lastErr := sy.Last()
	assert.NoError(t, lastErr)
	assert.Equal(t, "1.2.3", last.Version)
}

func TestSyncReadsAPrivateGitHubRelease(t *testing.T) {
	sg := newSigner(t, 2)
	src := newSource(t, makeRelease(t, sg, "2.0.0"))
	src.asRepo = true
	sy, dir := newSyncer(t, src, sg)
	_, err := sy.Sync(t.Context())
	require.NoError(t, err)
	assert.Equal(t, "2.0.0", published(t, dir).Version)

	// Without the token a private release is not found: nothing is published.
	dir2 := filepath.Join(t.TempDir(), "d")
	noToken, err := New(Config{SourceURL: src.url(), PublicKey: sg.publicKey(), Dir: dir2}, src.srv.Client())
	require.NoError(t, err)
	_, err = noToken.Sync(t.Context())
	require.Error(t, err)
	assert.NotContains(t, err.Error(), testToken)
	_, statErr := os.Stat(filepath.Join(dir2, "manifest.json"))
	assert.True(t, os.IsNotExist(statErr))
}

func TestSyncRefusesATamperedFileAndKeepsTheLastGoodRelease(t *testing.T) {
	sg := newSigner(t, 3)
	src := newSource(t, makeRelease(t, sg, "1.0.0"))
	sy, dir := newSyncer(t, src, sg)
	_, err := sy.Sync(t.Context())
	require.NoError(t, err)

	src.mu.Lock()
	src.rel = makeRelease(t, sg, "1.1.0")
	src.tamper = func(name string, data []byte) []byte {
		if strings.HasSuffix(name, ".tar.gz") {
			return append([]byte("evil"), data[4:]...)
		}
		return data
	}
	src.mu.Unlock()
	_, err = sy.Sync(t.Context())
	require.ErrorIs(t, err, ErrChecksum)

	m := published(t, dir)
	assert.Equal(t, "1.0.0", m.Version, "the last good release stays")
	assert.NotContains(t, names(t, dir), "asc-1.1.0-linux-x86_64.tar.gz", "nothing of the refused release is published")
	for _, n := range names(t, dir) {
		assert.False(t, strings.HasPrefix(n, ".sync-"), "the staging directory is removed")
	}
	_, lastErr := sy.Last()
	assert.ErrorIs(t, lastErr, ErrChecksum)
	last, _ := sy.Last()
	assert.Equal(t, "1.0.0", last.Version)
}

func TestSyncRefusesWhatTheConfiguredKeyDidNotSign(t *testing.T) {
	sg, other := newSigner(t, 4), newSigner(t, 5)

	for name, tc := range map[string]struct {
		rel  release
		want error
	}{
		"checksums signed by another key": {makeRelease(t, other, "1.0.0", func(m *ascdownload.Manifest, _ map[string][]byte, _ *signer) { m.MinisignPublicKey = "" }), ErrBadSignature},
		"manifest names another key":      {makeRelease(t, other, "1.0.0"), ErrKeyMismatch},
		"manifest without checksums": {makeRelease(t, sg, "1.0.0", func(m *ascdownload.Manifest, _ map[string][]byte, _ *signer) {
			m.Checksums, m.ChecksumsSignature = "", ""
		}), ErrUnsigned},
		"a file signed by another key": {makeRelease(t, sg, "1.0.0", func(m *ascdownload.Manifest, files map[string][]byte, _ *signer) {
			files[m.Files[0].Signature] = []byte(other.sign(files[m.Files[0].Name], true))
		}), ErrBadSignature},
		"a file whose hash is not in the signed checksums": {makeRelease(t, sg, "1.0.0", func(m *ascdownload.Manifest, files map[string][]byte, _ *signer) {
			files[m.Files[0].Name] = []byte("another content")
			m.Files[0].SHA256, m.Files[0].Size = "0000000000000000000000000000000000000000000000000000000000000000", int64(len("another content"))
		}), ErrChecksum},
	} {
		src := newSource(t, tc.rel)
		sy, dir := newSyncer(t, src, sg)
		_, err := sy.Sync(t.Context())
		require.ErrorIs(t, err, tc.want, name)
		_, statErr := os.Stat(filepath.Join(dir, "manifest.json"))
		assert.True(t, os.IsNotExist(statErr), name+": nothing is published")
	}
}

func TestSyncTamperedManifestAndChecksums(t *testing.T) {
	sg := newSigner(t, 6)
	src := newSource(t, makeRelease(t, sg, "1.0.0"))
	sy, dir := newSyncer(t, src, sg)

	// A manifest changed on the way, not the one the signed checksums cover.
	src.tamper = func(name string, data []byte) []byte {
		if name == "manifest.json" {
			return []byte(strings.Replace(string(data), "1.0.0", "9.9.9", 1))
		}
		return data
	}
	_, err := sy.Sync(t.Context())
	require.ErrorIs(t, err, ErrChecksum)

	// Checksums changed on the way: the signature no longer verifies.
	src.tamper = func(name string, data []byte) []byte {
		if name == "SHA256SUMS" {
			return append([]byte("0000000000000000000000000000000000000000000000000000000000000000  evil\n"), data...)
		}
		return data
	}
	_, err = sy.Sync(t.Context())
	require.ErrorIs(t, err, ErrBadSignature)
	_, statErr := os.Stat(filepath.Join(dir, "manifest.json"))
	assert.True(t, os.IsNotExist(statErr))
}

func TestSyncSizeCaps(t *testing.T) {
	sg := newSigner(t, 7)
	src := newSource(t, makeRelease(t, sg, "1.0.0"))
	dir := filepath.Join(t.TempDir(), "d")

	small, err := New(Config{SourceURL: src.url(), Token: testToken, PublicKey: sg.publicKey(), Dir: dir, MaxFileBytes: 10}, src.srv.Client())
	require.NoError(t, err)
	_, err = small.Sync(t.Context())
	require.ErrorContains(t, err, "larger than the limit")

	total, err := New(Config{SourceURL: src.url(), Token: testToken, PublicKey: sg.publicKey(), Dir: dir, MaxTotalBytes: 100}, src.srv.Client())
	require.NoError(t, err)
	_, err = total.Sync(t.Context())
	require.ErrorContains(t, err, "release is larger")

	// A source that sends more than it said is cut off and refused.
	src.oversize = true
	sy, _ := newSyncer(t, src, sg)
	_, err = sy.Sync(t.Context())
	require.Error(t, err)
	assert.NoFileExists(t, filepath.Join(dir, "manifest.json"))
}

func TestSyncReplacesAnOlderReleaseAndRemovesItsFiles(t *testing.T) {
	sg := newSigner(t, 8)
	src := newSource(t, makeRelease(t, sg, "1.0.0"))
	sy, dir := newSyncer(t, src, sg)
	_, err := sy.Sync(t.Context())
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "operator-notes.txt"), []byte("mine"), 0o644))

	src.mu.Lock()
	src.rel = makeRelease(t, sg, "1.1.0")
	src.mu.Unlock()
	res, err := sy.Sync(t.Context())
	require.NoError(t, err)
	assert.True(t, res.Changed)
	assert.Equal(t, "1.1.0", published(t, dir).Version)
	assert.NotContains(t, names(t, dir), "asc-1.0.0-linux-x86_64.tar.gz", "the files of the replaced release go")
	assert.Contains(t, names(t, dir), "operator-notes.txt", "other files of the directory are never touched")
}

func TestNewChecksTheSetting(t *testing.T) {
	sg := newSigner(t, 9)
	dir := t.TempDir()
	ok := Config{SourceURL: "https://example.com/rel", PublicKey: sg.publicKey(), Dir: dir}
	_, err := New(ok, nil)
	require.NoError(t, err)
	for name, mutate := range map[string]func(*Config){
		"http":        func(c *Config) { c.SourceURL = "http://example.com/rel" },
		"credentials": func(c *Config) { c.SourceURL = "https://u:p@example.com/rel" },
		"query":       func(c *Config) { c.SourceURL = "https://example.com/rel?x=1" },
		"no key":      func(c *Config) { c.PublicKey = "" },
		"bad key":     func(c *Config) { c.PublicKey = "not a key" },
		"relative":    func(c *Config) { c.Dir = "relative" },
		"bad token":   func(c *Config) { c.Token = "a b" },
	} {
		c := ok
		mutate(&c)
		_, err := New(c, nil)
		assert.Error(t, err, name)
	}
}

// The token goes to the host of the source only: a redirect to another host does not carry it.
func TestTheTokenDoesNotFollowARedirectToAnotherHost(t *testing.T) {
	var mu sync.Mutex
	var seen []string
	note := func(s string) { mu.Lock(); seen = append(seen, s); mu.Unlock() }
	other := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		note("other:" + r.Header.Get("Authorization"))
		_, _ = w.Write([]byte("content"))
	}))
	defer other.Close()
	first := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		note("first:" + r.Header.Get("Authorization"))
		// example.com is a name the certificate of the test servers covers.
		http.Redirect(w, r, "https://example.com:"+strings.Split(other.Listener.Addr().String(), ":")[1]+"/x", http.StatusFound)
	}))
	defer first.Close()

	client := first.Client()
	tr := client.Transport.(*http.Transport).Clone()
	tr.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		var d net.Dialer
		if strings.HasPrefix(addr, "example.com:") {
			return d.DialContext(ctx, network, other.Listener.Addr().String())
		}
		return d.DialContext(ctx, network, addr)
	}
	client.Transport = tr

	sg := newSigner(t, 10)
	sy, err := New(Config{SourceURL: first.URL + "/rel", Token: testToken, PublicKey: sg.publicKey(), Dir: t.TempDir()}, client)
	require.NoError(t, err)
	data, err := readAll(context.Background(), sy.source(), "manifest.json", 100)
	require.NoError(t, err)
	assert.Equal(t, "content", string(data))
	mu.Lock()
	defer mu.Unlock()
	require.Len(t, seen, 2)
	assert.Equal(t, "first:Bearer "+testToken, seen[0])
	assert.Equal(t, "other:", seen[1], "no token on the other host")
}

func TestTheClientRefusesInternalAddressesAndPlainRedirects(t *testing.T) {
	c := NewClient()
	for _, target := range []string{"https://127.0.0.1:1/x", "https://10.0.0.5/x", "https://169.254.169.254/latest/meta-data/", "https://[::1]:1/x"} {
		req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, target, http.NoBody)
		require.NoError(t, err)
		resp, err := c.Do(req)
		if resp != nil {
			resp.Body.Close()
		}
		require.Error(t, err, target)
		assert.ErrorContains(t, err, "asc download", target)
	}
	req, _ := http.NewRequest(http.MethodGet, "http://example.com/", http.NoBody)
	assert.Error(t, checkRedirect(req, nil), "a redirect to plain http")
}
