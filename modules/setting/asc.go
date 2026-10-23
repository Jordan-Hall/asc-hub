// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package setting

import (
	"fmt"
	"net/url"
	"strings"

	"forgejo.org/modules/log"
)

// Asc holds the [asc] settings: the integration with `asc serve`.
var Asc = struct {
	// Enabled turns on the internal authz endpoint and /.well-known/asc.
	Enabled bool `ini:"ENABLED"`
	// ServerURL is the public base URL of `asc serve`, announced by /.well-known/asc.
	ServerURL string `ini:"SERVER_URL"`
	// ShadowSamplePercent is the share (0 to 100) of read calls of a storage_backend=dual repository that
	// are repeated against asc and compared (modules/git/dual). Zero turns the shadow off.
	ShadowSamplePercent int `ini:"SHADOW_SAMPLE_PERCENT"`
}{}

// DefaultAscShadowSamplePercent is the default of [asc] SHADOW_SAMPLE_PERCENT.
const DefaultAscShadowSamplePercent = 10

func loadAscFrom(rootCfg ConfigProvider) {
	Asc.Enabled = false
	Asc.ServerURL = ""
	Asc.ShadowSamplePercent = DefaultAscShadowSamplePercent
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
	serverURL, err := validateAscServerURL(Asc.ServerURL)
	if err != nil {
		log.Fatal("Invalid [asc] SERVER_URL: %v", err)
	}
	Asc.ServerURL = serverURL
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
