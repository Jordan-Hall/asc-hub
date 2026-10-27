// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

// Package ascdownloadsync keeps the download directory of the hub ([asc] DOWNLOAD_DIR) up to date from a release
// source ([asc] DOWNLOAD_SOURCE_URL): a private GitHub release, which the hub cannot link to, or any HTTPS directory.
//
// # Trust
//
// The source is not trusted; the release public key in the configuration ([asc] DOWNLOAD_PUBLIC_KEY) is. A release
// is published only when all of this holds:
//
//   - the manifest names a SHA256SUMS file and its minisign signature, and the signature verifies with the
//     configured key (a key named in the manifest must be the configured one);
//   - SHA256SUMS lists manifest.json, and the manifest that was fetched has that hash;
//   - every file of the manifest has the hash and the size the manifest says, which SHA256SUMS repeats, and the
//     signature of the file, if the manifest lists one, verifies with the configured key.
//
// Nothing is written to the download directory until every file has been fetched, hashed and checked in a staging
// directory below it. Then the files are moved in one by one, each rename atomic, and manifest.json last, so a
// visitor sees the old release or the new one. A failure at any step leaves the last good release in place.
//
// # The source
//
// The URL is an https address. If it is the API address of a release (".../repos/{owner}/{repo}/releases/latest",
// ".../releases/tags/{tag}" or ".../releases/{id}", also on GitHub Enterprise) the assets of that release are
// fetched through the API with the token, which is how a private release is read. Any other URL is a directory:
// manifest.json and the files are fetched from below it. The token is sent only to the host of the URL; a redirect
// to another host (GitHub's storage) does not carry it. Every connection goes through a dialer that refuses
// private, loopback and link-local addresses, so the source cannot be pointed at the inside of the network.
package ascdownloadsync

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"forgejo.org/modules/ascdownload"
	"forgejo.org/modules/hostmatcher"
	"forgejo.org/modules/log"
	"forgejo.org/modules/proxy"

	"golang.org/x/crypto/blake2b"
)

// Named limits. Each bounds the size, count or time of something the source influences.
const (
	// MaxManifestBytes bounds manifest.json (the hub's reader allows 1 MiB).
	MaxManifestBytes = 1 << 20
	// MaxSumsBytes bounds the SHA256SUMS file.
	MaxSumsBytes = 1 << 20
	// MaxSignatureBytes bounds a .minisig file.
	MaxSignatureBytes = 4 << 10
	// MaxReleaseJSONBytes bounds the answer of the release API.
	MaxReleaseJSONBytes = 4 << 20
	// MaxAssets bounds the assets of a release the hub reads.
	MaxAssets = 500
	// DefaultMaxFileBytes bounds one file of a release.
	DefaultMaxFileBytes = int64(1) << 30
	// DefaultMaxTotalBytes bounds all the files of a release.
	DefaultMaxTotalBytes = int64(4) << 30
	// DefaultInterval is how often the source is looked at.
	DefaultInterval = time.Hour
	// MinInterval is the shortest interval accepted.
	MinInterval = time.Minute
	// maxLegacyBytes bounds a file with a legacy (not prehashed) signature, which is checked in memory.
	maxLegacyBytes = 64 << 20
	maxRedirects   = 5
	requestTimeout = 30 * time.Minute
)

var (
	// ErrUnsigned means the manifest does not name the checksum file and its signature.
	ErrUnsigned = errors.New("the manifest does not name a signed SHA256SUMS file")
	// ErrKeyMismatch means the manifest names another public key than the configured one.
	ErrKeyMismatch = errors.New("the manifest names another release public key than the configured one")
	// ErrChecksum means a file, or the manifest, does not have the hash that the signed checksums list.
	ErrChecksum = errors.New("a file does not match the signed checksums")
)

// Config is the setting of one syncer.
type Config struct {
	// SourceURL is an https address; see the package documentation.
	SourceURL string
	// Token is sent to the host of SourceURL as a bearer credential. Empty means none.
	Token string
	// PublicKey is the minisign public key every signature must verify with.
	PublicKey string
	// Dir is the download directory.
	Dir string
	// MaxFileBytes and MaxTotalBytes bound the files; zero means the defaults.
	MaxFileBytes, MaxTotalBytes int64
}

// Result says what a run did.
type Result struct {
	Version string
	// Changed is false when the release in the directory was already the one at the source.
	Changed bool
	At      time.Time
}

// Syncer syncs a release. It is safe for concurrent use; runs are serialised.
type Syncer struct {
	cfg    Config
	key    PublicKey
	base   *url.URL
	client *http.Client

	mu      sync.Mutex
	lastOK  Result
	lastErr error
}

// NewClient returns the HTTP client of a syncer: it refuses redirects to anything but https and connections to
// anything but an external address.
func NewClient() *http.Client {
	allow := hostmatcher.ParseHostMatchList("", hostmatcher.MatchBuiltinExternal)
	block := hostmatcher.ParseHostMatchList("", "")
	return &http.Client{
		Transport: &http.Transport{
			Proxy:                 proxy.Proxy(),
			DialContext:           hostmatcher.NewDialContext("asc download", allow, block, nil),
			TLSHandshakeTimeout:   30 * time.Second,
			ResponseHeaderTimeout: 60 * time.Second,
		},
		CheckRedirect: checkRedirect,
	}
}

func checkRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= maxRedirects {
		return errors.New("too many redirects")
	}
	if req.URL.Scheme != "https" {
		return errors.New("a redirect to a address that is not https")
	}
	return nil
}

// New checks the setting and returns a syncer. client is the HTTP client; nil means NewClient().
func New(cfg Config, client *http.Client) (*Syncer, error) {
	u, err := url.Parse(strings.TrimSpace(cfg.SourceURL))
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return nil, errors.New("the download source must be an https address without credentials, query or fragment")
	}
	if cfg.Dir == "" || !filepath.IsAbs(cfg.Dir) {
		return nil, errors.New("the download directory must be an absolute path")
	}
	key, err := ParsePublicKey(cfg.PublicKey)
	if err != nil {
		return nil, err
	}
	if strings.ContainsAny(cfg.Token, " \r\n\t\x00") {
		return nil, errors.New("the download source token has a space or a control character")
	}
	if cfg.MaxFileBytes <= 0 {
		cfg.MaxFileBytes = DefaultMaxFileBytes
	}
	if cfg.MaxTotalBytes <= 0 {
		cfg.MaxTotalBytes = DefaultMaxTotalBytes
	}
	if client == nil {
		client = NewClient()
	}
	if client.CheckRedirect == nil {
		c := *client
		c.CheckRedirect = checkRedirect
		client = &c
	}
	return &Syncer{cfg: cfg, key: key, base: u, client: client}, nil
}

// Last returns the result of the last good run and the error of the last run, if it failed.
func (s *Syncer) Last() (Result, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastOK, s.lastErr
}

// Run syncs now and then every interval until ctx ends. It logs; it never panics out of a run.
func (s *Syncer) Run(ctx context.Context, interval time.Duration) {
	if interval < MinInterval {
		interval = DefaultInterval
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		s.runOnce(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

func (s *Syncer) runOnce(ctx context.Context) {
	defer func() {
		if r := recover(); r != nil {
			log.Error("asc download sync: panic: %v", r)
		}
	}()
	res, err := s.Sync(ctx)
	switch {
	case err != nil:
		log.Warn("asc download sync: the last good release stays; this run failed: %v", err)
	case res.Changed:
		log.Info("asc download sync: published release %s", res.Version)
	default:
		log.Trace("asc download sync: release %s is current", res.Version)
	}
}

// ----------------------------------------------------------------------------------------------------- source

type source interface {
	// open returns the content of the named file and its size (-1 when unknown), cut at limit+1 bytes.
	open(ctx context.Context, name string, limit int64) (io.ReadCloser, int64, error)
}

type limited struct {
	io.Reader
	io.Closer
}

func (s *Syncer) get(ctx context.Context, rawURL, accept string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, http.NoBody)
	if err != nil {
		return nil, err
	}
	if accept != "" {
		req.Header.Set("Accept", accept)
	}
	req.Header.Set("User-Agent", "asc-hub-download-sync")
	// Only the host of the source gets the token; the client does not forward it across hosts on a redirect.
	if s.cfg.Token != "" && req.URL.Host == s.base.Host {
		req.Header.Set("Authorization", "Bearer "+s.cfg.Token)
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching %s: %w", redact(rawURL), unwrapURLError(err))
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("fetching %s: status %d", redact(rawURL), resp.StatusCode)
	}
	return resp, nil
}

// unwrapURLError drops the URL of a *url.Error, which could carry a signed address.
func unwrapURLError(err error) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		return ue.Err
	}
	return err
}

// redact keeps scheme, host and path of an address for a message: a signed query is dropped.
func redact(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return "the source"
	}
	return u.Scheme + "://" + u.Host + u.Path
}

type dirSource struct{ s *Syncer }

func (d dirSource) open(ctx context.Context, name string, limit int64) (io.ReadCloser, int64, error) {
	resp, err := d.s.get(ctx, d.s.base.JoinPath(name).String(), "")
	if err != nil {
		return nil, 0, err
	}
	return limited{io.LimitReader(resp.Body, limit+1), resp.Body}, resp.ContentLength, nil
}

var releaseAPIRe = regexp.MustCompile(`(?:^|/)repos/[^/]+/[^/]+/releases/(?:latest|tags/[^/]+|[0-9]+)$`)

type releaseSource struct {
	s      *Syncer
	assets map[string]string
}

type releaseJSON struct {
	Assets []struct {
		Name string `json:"name"`
		URL  string `json:"url"`
	} `json:"assets"`
}

func (r *releaseSource) load(ctx context.Context) error {
	if r.assets != nil {
		return nil
	}
	resp, err := r.s.get(ctx, r.s.base.String(), "application/vnd.github+json")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, MaxReleaseJSONBytes+1))
	if err != nil || len(data) > MaxReleaseJSONBytes {
		return errors.New("the release description is unreadable or too large")
	}
	var rel releaseJSON
	if err := json.Unmarshal(data, &rel); err != nil {
		return errors.New("the release description is not valid JSON")
	}
	if len(rel.Assets) > MaxAssets {
		return errors.New("the release has too many assets")
	}
	r.assets = make(map[string]string, len(rel.Assets))
	for _, a := range rel.Assets {
		u, err := url.Parse(a.URL)
		if err != nil || u.Scheme != "https" || u.Host == "" {
			continue
		}
		r.assets[a.Name] = a.URL
	}
	return nil
}

func (r *releaseSource) open(ctx context.Context, name string, limit int64) (io.ReadCloser, int64, error) {
	if err := r.load(ctx); err != nil {
		return nil, 0, err
	}
	assetURL, ok := r.assets[name]
	if !ok {
		return nil, 0, fmt.Errorf("the release has no asset %s", name)
	}
	resp, err := r.s.get(ctx, assetURL, "application/octet-stream")
	if err != nil {
		return nil, 0, err
	}
	return limited{io.LimitReader(resp.Body, limit+1), resp.Body}, resp.ContentLength, nil
}

func (s *Syncer) source() source {
	if releaseAPIRe.MatchString(s.base.Path) {
		return &releaseSource{s: s}
	}
	return dirSource{s: s}
}

// readAll reads a small file of the source, refusing more than limit bytes.
func readAll(ctx context.Context, src source, name string, limit int64) ([]byte, error) {
	rc, _, err := src.open(ctx, name, limit)
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	data, err := io.ReadAll(rc)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", name, unwrapURLError(err))
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("%s is larger than %d bytes", name, limit)
	}
	return data, nil
}

// ----------------------------------------------------------------------------------------------------- sync

// parseSums reads a SHA256SUMS file: "<64 hex>  <name>" or "<64 hex> *<name>" per line.
func parseSums(data []byte) (map[string]string, error) {
	out := map[string]string{}
	for _, line := range strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		sum, name, ok := strings.Cut(line, " ")
		name = strings.TrimPrefix(strings.TrimLeft(name, " "), "*")
		if !ok || len(sum) != 64 || name == "" || strings.ContainsAny(name, "/\\") {
			return nil, errors.New("SHA256SUMS has a line that is not '<sha256>  <file name>'")
		}
		if _, err := hex.DecodeString(sum); err != nil {
			return nil, errors.New("SHA256SUMS has a hash that is not hexadecimal")
		}
		if prev, dup := out[name]; dup && prev != strings.ToLower(sum) {
			return nil, fmt.Errorf("SHA256SUMS lists %s twice with different hashes", name)
		}
		out[name] = strings.ToLower(sum)
	}
	return out, nil
}

type staged struct {
	name string
	path string
}

// Sync looks at the source once and publishes a newer or changed release. It returns the release now in the
// directory. On an error the directory is unchanged.
func (s *Syncer) Sync(ctx context.Context) (res Result, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	defer func() {
		s.lastErr = err
		if err == nil {
			s.lastOK = res
		}
	}()
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	src := s.source()
	manifestBytes, err := readAll(ctx, src, ascdownload.ManifestName, MaxManifestBytes)
	if err != nil {
		return Result{}, err
	}
	m, err := ascdownload.Parse(bytes.NewReader(manifestBytes))
	if err != nil {
		return Result{}, err
	}
	if m.Checksums == "" || m.ChecksumsSignature == "" {
		return Result{}, ErrUnsigned
	}
	if m.MinisignPublicKey != "" {
		theirs, err := ParsePublicKey(m.MinisignPublicKey)
		if err != nil || theirs.id != s.key.id || !bytes.Equal(theirs.key, s.key.key) {
			return Result{}, ErrKeyMismatch
		}
	}

	// The checksums, signed by the key of the configuration.
	sums, err := readAll(ctx, src, m.Checksums, MaxSumsBytes)
	if err != nil {
		return Result{}, err
	}
	sigText, err := readAll(ctx, src, m.ChecksumsSignature, MaxSignatureBytes)
	if err != nil {
		return Result{}, err
	}
	sig, err := ParseSignature(string(sigText))
	if err != nil {
		return Result{}, err
	}
	if err := s.key.VerifyBytes(sig, sums); err != nil {
		return Result{}, err
	}
	listed, err := parseSums(sums)
	if err != nil {
		return Result{}, err
	}
	// The manifest the files are chosen from is the one the signed checksums cover.
	mh := sha256.Sum256(manifestBytes)
	if listed[ascdownload.ManifestName] != hex.EncodeToString(mh[:]) {
		return Result{}, fmt.Errorf("%w: manifest.json", ErrChecksum)
	}
	var total int64
	for _, f := range m.Files {
		if f.Size <= 0 || f.Size > s.cfg.MaxFileBytes {
			return Result{}, fmt.Errorf("%s is empty or larger than the limit of %d bytes", f.Name, s.cfg.MaxFileBytes)
		}
		if total += f.Size; total > s.cfg.MaxTotalBytes {
			return Result{}, fmt.Errorf("the release is larger than the limit of %d bytes", s.cfg.MaxTotalBytes)
		}
		if listed[f.Name] != f.SHA256 {
			return Result{}, fmt.Errorf("%w: %s", ErrChecksum, f.Name)
		}
	}

	if s.current(manifestBytes, m) {
		return Result{Version: m.Version, Changed: false, At: time.Now()}, nil
	}

	if err := os.MkdirAll(s.cfg.Dir, 0o755); err != nil {
		return Result{}, fmt.Errorf("the download directory: %w", err)
	}
	stage, err := os.MkdirTemp(s.cfg.Dir, ".sync-")
	if err != nil {
		return Result{}, fmt.Errorf("the staging directory: %w", err)
	}
	defer os.RemoveAll(stage)

	var files []staged
	put := func(name string, data []byte) error {
		p := filepath.Join(stage, name)
		if err := os.WriteFile(p, data, 0o644); err != nil {
			return err
		}
		files = append(files, staged{name, p})
		return nil
	}
	for _, f := range m.Files {
		p := filepath.Join(stage, f.Name)
		digest, err := s.fetchFile(ctx, src, f, p)
		if err != nil {
			return Result{}, err
		}
		files = append(files, staged{f.Name, p})
		if f.Signature != "" {
			if err := s.fetchSignature(ctx, src, f, p, digest, put); err != nil {
				return Result{}, err
			}
		}
	}
	if err := put(m.Checksums, sums); err != nil {
		return Result{}, err
	}
	if err := put(m.ChecksumsSignature, sigText); err != nil {
		return Result{}, err
	}
	// manifest.json goes in last: it is the commit point.
	if err := os.WriteFile(filepath.Join(stage, ascdownload.ManifestName), manifestBytes, 0o644); err != nil {
		return Result{}, err
	}
	// The staged release must read back as a whole before anything is published.
	if _, err := ascdownload.Load(stage, true); err != nil {
		return Result{}, fmt.Errorf("the staged release is not usable: %w", err)
	}

	old, _ := ascdownload.Load(s.cfg.Dir, false)
	for _, f := range files {
		if err := os.Rename(f.path, filepath.Join(s.cfg.Dir, f.name)); err != nil {
			return Result{}, fmt.Errorf("publishing %s: %w", f.name, err)
		}
	}
	if err := os.Rename(filepath.Join(stage, ascdownload.ManifestName), filepath.Join(s.cfg.Dir, ascdownload.ManifestName)); err != nil {
		return Result{}, fmt.Errorf("publishing manifest.json: %w", err)
	}
	ascdownload.ResetCache()
	// Files of the release that was replaced and that the new one does not use go; nothing else in the directory is
	// touched.
	if old != nil {
		keep := m.ServedNames()
		for name := range old.ServedNames() {
			if !keep[name] && name != ascdownload.ManifestName {
				_ = os.Remove(filepath.Join(s.cfg.Dir, name))
			}
		}
	}
	return Result{Version: m.Version, Changed: true, At: time.Now()}, nil
}

// current reports whether the directory already holds exactly this release.
func (s *Syncer) current(manifestBytes []byte, m *ascdownload.Manifest) bool {
	have, err := os.ReadFile(filepath.Join(s.cfg.Dir, ascdownload.ManifestName))
	if err != nil || !bytes.Equal(have, manifestBytes) {
		return false
	}
	_, err = ascdownload.Load(s.cfg.Dir, true)
	return err == nil
}

// fetchFile downloads one file to path, checking its size and its SHA-256 as it comes. It returns the BLAKE2b-512
// digest too, which a prehashed signature covers.
func (s *Syncer) fetchFile(ctx context.Context, src source, f ascdownload.File, path string) ([]byte, error) {
	rc, _, err := src.open(ctx, f.Name, f.Size)
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	out, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return nil, err
	}
	sha := sha256.New()
	b2, _ := blake2b.New512(nil)
	n, err := io.Copy(io.MultiWriter(out, sha, b2), rc)
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return nil, fmt.Errorf("downloading %s: %w", f.Name, unwrapURLError(err))
	}
	if n != f.Size {
		return nil, fmt.Errorf("%w: %s has %d bytes, the manifest says %d", ErrChecksum, f.Name, n, f.Size)
	}
	if hex.EncodeToString(sha.Sum(nil)) != f.SHA256 {
		return nil, fmt.Errorf("%w: %s", ErrChecksum, f.Name)
	}
	return b2.Sum(nil), nil
}

// fetchSignature downloads the detached signature of f and verifies the staged file with the configured key.
func (s *Syncer) fetchSignature(ctx context.Context, src source, f ascdownload.File, path string, digest []byte, put func(string, []byte) error) error {
	text, err := readAll(ctx, src, f.Signature, MaxSignatureBytes)
	if err != nil {
		return err
	}
	sig, err := ParseSignature(string(text))
	if err != nil {
		return fmt.Errorf("%s: %w", f.Signature, err)
	}
	switch {
	case sig.Prehashed():
		err = s.key.Verify(sig, nil, digest)
	case f.Size > maxLegacyBytes:
		return fmt.Errorf("%s has a legacy signature and is too large to check", f.Name)
	default:
		data, rerr := os.ReadFile(path)
		if rerr != nil {
			return rerr
		}
		err = s.key.Verify(sig, data, nil)
	}
	if err != nil {
		return fmt.Errorf("%s: %w", f.Name, err)
	}
	return put(f.Signature, text)
}
