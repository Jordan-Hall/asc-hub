// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package setting

import (
	"fmt"
	"net"
	"net/url"
	"strings"
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
	sec := rootCfg.Section("asc")
	if err := sec.MapTo(&Asc); err != nil {
		log.Fatal("Failed to map [asc] settings: %v", err)
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
