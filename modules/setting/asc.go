// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package setting

import (
	"fmt"
	"net"
	"net/url"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	"forgejo.org/modules/log"
)

// Asc holds the [asc] settings: the integration with `asc serve`.
var Asc = struct {
	// Enabled turns on the internal authz endpoint and /.well-known/asc.
	Enabled bool `ini:"ENABLED"`
	// ServerURL is the base URL Forgejo itself dials `asc serve` on (a loopback address is fine).
	ServerURL string `ini:"SERVER_URL"`
	// PublicURL is the base URL asc clients and Actions runners are told to use, announced by
	// /.well-known/asc. It defaults to ServerURL; set it when Forgejo reaches asc on an address that
	// outside clients cannot (for example the hub's own root URL when it proxies asc). Use AscPublicURL.
	PublicURL string `ini:"PUBLIC_URL"`
	// ShadowSamplePercent is the share (0 to 100) of read calls of a storage_backend=dual repository that
	// are repeated against asc and compared (modules/git/dual). Zero turns the shadow off.
	ShadowSamplePercent int `ini:"SHADOW_SAMPLE_PERCENT"`
	// Writer chooses who writes a storage_backend=asc repository (SPEC 13.1): "git" keeps the temp-repository
	// pushes into the git bridge that asc imports (Stages 2 and 3), "asc" writes through asc.v1 (Stage 4, K12).
	Writer string `ini:"WRITER"`
	// Bridge says how the git bridge of an asc repository is kept: "eager" (asc's outbox keeps it current),
	// "lazy" (Forgejo builds it with `asc export --git` on the first legacy git request) or "off" (no git
	// access to asc repositories through Forgejo). See services/ascbridge (K13).
	Bridge string `ini:"BRIDGE"`
	// NewRepositories chooses the storage backend of a repository created after this setting is on: "git" (the
	// default) or "asc". It takes effect only with [asc] WRITER = asc; migrations and mirrors stay git.
	NewRepositories string `ini:"NEW_REPOSITORIES"`
	// AscBinary is the `asc` executable that builds a lazy bridge.
	AscBinary string `ini:"ASC_BINARY"`
	// BridgeWorkspaces is the directory that holds the asc workspaces a lazy bridge is built from.
	BridgeWorkspaces string `ini:"BRIDGE_WORKSPACES"`
	// DownloadDir is the directory of the asc releases this hub offers on /download: a manifest.json and the
	// files it lists. It is served read-only under /-/download/. Empty means the hub offers no download.
	// Unlike the rest of [asc] it works with ENABLED false.
	DownloadDir string `ini:"DOWNLOAD_DIR"`
	// DownloadBaseURL is the base the download links point at. Empty means this hub's own /-/download/ mount.
	// It must be https (http only for a loopback host), with no credentials, query or fragment.
	DownloadBaseURL string `ini:"DOWNLOAD_BASE_URL"`
	// DownloadSourceURL is where the hub fetches the signed release files into DownloadDir (services/ascdownloadsync):
	// an https address, either the API address of a (private) GitHub release or a directory that holds manifest.json
	// and the files. Empty means the hub does not fetch; the directory is filled by other means.
	DownloadSourceURL string `ini:"DOWNLOAD_SOURCE_URL"`
	// DownloadSourceToken is sent to the host of DownloadSourceURL as a bearer credential (a token that may read a
	// private release). It is set by DOWNLOAD_SOURCE_TOKEN or read from the file named by DOWNLOAD_SOURCE_TOKEN_URI.
	DownloadSourceToken string `ini:"-"`
	// DownloadPublicKey is the minisign public key (the key line of the .pub file) that the checksums and the files
	// of a fetched release must be signed with. It is required with DownloadSourceURL.
	DownloadPublicKey string `ini:"DOWNLOAD_PUBLIC_KEY"`
	// DownloadSyncInterval is how often the source is looked at; the default is one hour, the least one minute.
	DownloadSyncInterval time.Duration `ini:"DOWNLOAD_SYNC_INTERVAL"`
	// SSHEnabled allows the hub's SSH server and every SSH instruction in the UI. It is false by default:
	// an ASC Hub works with HTTPS alone (asc and git both speak HTTPS to it). It only has an effect with
	// [asc] ENABLED = true, where a false value forces [server] DISABLE_SSH = true. See SSHEnabled.
	SSHEnabled bool `ini:"SSH_ENABLED"`
}{}

// Values of [asc] WRITER and [asc] BRIDGE.
const (
	AscWriterGit = "git"
	AscWriterAsc = "asc"

	AscBridgeOff   = "off"
	AscBridgeLazy  = "lazy"
	AscBridgeEager = "eager"
)

// DefaultAscShadowSamplePercent is the default of [asc] SHADOW_SAMPLE_PERCENT.
const DefaultAscShadowSamplePercent = 10

func loadAscFrom(rootCfg ConfigProvider) {
	Asc.Enabled = false
	Asc.ServerURL = ""
	Asc.PublicURL = ""
	Asc.ShadowSamplePercent = DefaultAscShadowSamplePercent
	Asc.Writer = AscWriterGit
	Asc.Bridge = AscBridgeEager
	Asc.AscBinary = "asc"
	Asc.NewRepositories = AscWriterGit
	Asc.BridgeWorkspaces = ""
	Asc.DownloadDir = ""
	Asc.DownloadBaseURL = ""
	Asc.DownloadSourceURL = ""
	Asc.DownloadSourceToken = ""
	Asc.DownloadPublicKey = ""
	Asc.DownloadSyncInterval = time.Hour
	Asc.SSHEnabled = false
	sec := rootCfg.Section("asc")
	if err := sec.MapTo(&Asc); err != nil {
		log.Fatal("Failed to map [asc] settings: %v", err)
	}
	Asc.DownloadSourceToken = loadSecret(sec, "DOWNLOAD_SOURCE_TOKEN_URI", "DOWNLOAD_SOURCE_TOKEN")
	if err := normaliseAscDownload(); err != nil {
		log.Fatal("Invalid [asc] download settings: %v", err)
	}
	if !Asc.Enabled {
		return
	}
	if Asc.ShadowSamplePercent < 0 || Asc.ShadowSamplePercent > 100 {
		log.Fatal("Invalid [asc] SHADOW_SAMPLE_PERCENT: must be between 0 and 100")
	}
	switch Asc.Writer = strings.ToLower(strings.TrimSpace(Asc.Writer)); Asc.Writer {
	case AscWriterGit, AscWriterAsc:
	default:
		log.Fatal("Invalid [asc] WRITER %q: must be git or asc", Asc.Writer)
	}
	switch Asc.NewRepositories = strings.ToLower(strings.TrimSpace(Asc.NewRepositories)); Asc.NewRepositories {
	case AscWriterGit, AscWriterAsc:
	default:
		log.Fatal("Invalid [asc] NEW_REPOSITORIES %q: must be git or asc", Asc.NewRepositories)
	}
	switch Asc.Bridge = strings.ToLower(strings.TrimSpace(Asc.Bridge)); Asc.Bridge {
	case AscBridgeOff, AscBridgeLazy, AscBridgeEager:
	default:
		log.Fatal("Invalid [asc] BRIDGE %q: must be off, lazy or eager", Asc.Bridge)
	}
	if Asc.AscBinary = strings.TrimSpace(Asc.AscBinary); Asc.AscBinary == "" || strings.IndexFunc(Asc.AscBinary, unicode.IsControl) >= 0 {
		log.Fatal("Invalid [asc] ASC_BINARY")
	}
	serverURL, err := validateAscServerURL(Asc.ServerURL)
	if err != nil {
		log.Fatal("Invalid [asc] SERVER_URL: %v", err)
	}
	Asc.ServerURL = serverURL
	if strings.TrimSpace(Asc.PublicURL) == "" {
		Asc.PublicURL = ""
		return
	}
	publicURL, err := validateAscPublicURL(Asc.PublicURL)
	if err != nil {
		log.Fatal("Invalid [asc] PUBLIC_URL: %v", err)
	}
	Asc.PublicURL = publicURL
}

// SSHEnabled says whether git over SSH is offered: the SSH server may run, and clone panels, settings and
// documentation may show SSH instructions. With [asc] ENABLED = true it needs [asc] SSH_ENABLED = true as
// well as [server] DISABLE_SSH = false; loadSSHFrom enforces the first, so SSH.Disabled is the answer.
func SSHEnabled() bool {
	return !SSH.Disabled
}

// AscPublicURL is the asc URL to announce to clients: [asc] PUBLIC_URL, or SERVER_URL when it is not set.
func AscPublicURL() string {
	if Asc.PublicURL != "" {
		return Asc.PublicURL
	}
	return Asc.ServerURL
}

// validateAscPublicURL is validateAscServerURL for a URL that outside clients dial: it must also be https,
// except for a loopback host (a local setup), because a client sends its credentials there.
func validateAscPublicURL(raw string) (string, error) {
	normalised, err := validateAscServerURL(raw)
	if err != nil {
		return "", err
	}
	u, err := url.Parse(normalised)
	if err != nil {
		return "", err
	}
	if u.Scheme != "https" && !isLoopbackHost(u.Hostname()) {
		return "", fmt.Errorf("must use https unless the host is loopback")
	}
	return normalised, nil
}

func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// validateAscServerURL returns the normalised URL of asc serve (no trailing slash).
// It must be an absolute http(s) URL without credentials, query or fragment.
func validateAscServerURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("must be set when [asc] ENABLED is true")
	}
	if len(raw) > 2048 {
		return "", fmt.Errorf("is too long")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", err
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("must use http or https")
	}
	if u.Host == "" || u.Hostname() == "" {
		return "", fmt.Errorf("must have a host")
	}
	if u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" {
		return "", fmt.Errorf("must not contain credentials, a query or a fragment")
	}
	return strings.TrimRight(u.String(), "/"), nil
}

// normaliseAscDownload checks [asc] DOWNLOAD_DIR and DOWNLOAD_BASE_URL and stores them in their final form:
// the directory cleaned and absolute, the base URL with exactly one trailing slash.
func normaliseAscDownload() error {
	Asc.DownloadDir = strings.TrimSpace(Asc.DownloadDir)
	if Asc.DownloadDir != "" {
		if strings.IndexFunc(Asc.DownloadDir, unicode.IsControl) >= 0 {
			return fmt.Errorf("DOWNLOAD_DIR contains a control character")
		}
		if !filepath.IsAbs(Asc.DownloadDir) {
			return fmt.Errorf("DOWNLOAD_DIR must be an absolute path")
		}
		Asc.DownloadDir = filepath.Clean(Asc.DownloadDir)
	}
	Asc.DownloadBaseURL = strings.TrimSpace(Asc.DownloadBaseURL)
	if Asc.DownloadBaseURL != "" {
		base, err := validateAscPublicURL(Asc.DownloadBaseURL)
		if err != nil {
			return fmt.Errorf("DOWNLOAD_BASE_URL %w", err)
		}
		Asc.DownloadBaseURL = base + "/"
	}
	return normaliseAscDownloadSource()
}

// normaliseAscDownloadSource checks the settings of the release fetch. They go together: a source needs a
// directory to fill and the key its files are signed with, and a token needs a source.
func normaliseAscDownloadSource() error {
	Asc.DownloadSourceURL = strings.TrimSpace(Asc.DownloadSourceURL)
	Asc.DownloadSourceToken = strings.TrimSpace(Asc.DownloadSourceToken)
	Asc.DownloadPublicKey = strings.TrimSpace(Asc.DownloadPublicKey)
	if Asc.DownloadSyncInterval < time.Minute {
		if Asc.DownloadSyncInterval != 0 {
			return fmt.Errorf("DOWNLOAD_SYNC_INTERVAL must be at least one minute")
		}
		Asc.DownloadSyncInterval = time.Hour
	}
	if Asc.DownloadSourceURL == "" {
		if Asc.DownloadSourceToken != "" {
			return fmt.Errorf("DOWNLOAD_SOURCE_TOKEN needs DOWNLOAD_SOURCE_URL")
		}
		return nil
	}
	u, err := url.Parse(Asc.DownloadSourceURL)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return fmt.Errorf("DOWNLOAD_SOURCE_URL must be an https address without credentials, query or fragment")
	}
	if Asc.DownloadDir == "" {
		return fmt.Errorf("DOWNLOAD_SOURCE_URL needs DOWNLOAD_DIR")
	}
	if Asc.DownloadPublicKey == "" {
		return fmt.Errorf("DOWNLOAD_SOURCE_URL needs DOWNLOAD_PUBLIC_KEY: the key the release is signed with")
	}
	if strings.IndexFunc(Asc.DownloadSourceToken, func(r rune) bool { return r <= ' ' || r == 0x7f }) >= 0 {
		return fmt.Errorf("DOWNLOAD_SOURCE_TOKEN has a space or a control character")
	}
	return nil
}
