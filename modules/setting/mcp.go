// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package setting

import (
	"fmt"
	"net/url"
	"strings"

	"forgejo.org/modules/log"
)

// MaxMCPAllowedOrigins bounds the number of entries in [mcp] ALLOWED_ORIGINS.
const MaxMCPAllowedOrigins = 32

// MCP holds the [mcp] settings: the Model Context Protocol endpoint at /api/mcp.
var MCP = struct {
	// Enabled serves /api/mcp and its protected resource metadata document.
	Enabled bool `ini:"ENABLED"`
	// Write lets write tools appear and run. It is not enough alone: the lockdown mode must also be available
	// (routers/api/mcp), so a build without it keeps the endpoint read-only.
	Write bool `ini:"WRITE"`
	// AllowedOrigins lists the browser origins that may call the endpoint, besides the origin of AppURL. Each entry
	// is normalised by NormalizeMCPOrigin. There is no wildcard.
	AllowedOrigins []string `ini:"-"`
}{}

func loadMCPFrom(rootCfg ConfigProvider) {
	MCP.Enabled = true
	MCP.Write = false
	MCP.AllowedOrigins = nil
	sec := rootCfg.Section("mcp")
	if err := sec.MapTo(&MCP); err != nil {
		log.Fatal("Failed to map [mcp] settings: %v", err)
	}
	origins, err := parseMCPAllowedOrigins(sec.Key("ALLOWED_ORIGINS").String())
	if err != nil {
		log.Fatal("Invalid [mcp] ALLOWED_ORIGINS: %v", err)
	}
	MCP.AllowedOrigins = origins
}

// parseMCPAllowedOrigins parses a comma separated list of origins. An empty list is valid.
func parseMCPAllowedOrigins(raw string) ([]string, error) {
	var out []string
	for part := range strings.SplitSeq(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		origin, err := NormalizeMCPOrigin(part)
		if err != nil {
			return nil, fmt.Errorf("%q: %w", part, err)
		}
		out = append(out, origin)
		if len(out) > MaxMCPAllowedOrigins {
			return nil, fmt.Errorf("at most %d origins are allowed", MaxMCPAllowedOrigins)
		}
	}
	return out, nil
}

// NormalizeMCPOrigin returns scheme://host[:port] in lower case with a default port removed. The input must be an
// http or https origin: no credentials, path, query or fragment. "null" and wildcards are refused.
func NormalizeMCPOrigin(raw string) (string, error) {
	if len(raw) > 512 {
		return "", fmt.Errorf("is too long")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("is not a URL")
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return "", fmt.Errorf("must use http or https")
	}
	host := strings.ToLower(u.Hostname())
	if host == "" || strings.Contains(host, "*") {
		return "", fmt.Errorf("must have a host without wildcards")
	}
	if u.User != nil || (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.Opaque != "" {
		return "", fmt.Errorf("must be an origin: no credentials, path, query or fragment")
	}
	port := u.Port()
	if port == "" && strings.HasSuffix(u.Host, ":") {
		return "", fmt.Errorf("has an empty port")
	}
	if (scheme == "http" && port == "80") || (scheme == "https" && port == "443") {
		port = ""
	}
	hostPart := host
	if strings.Contains(host, ":") {
		hostPart = "[" + host + "]"
	}
	if port != "" {
		hostPart += ":" + port
	}
	return scheme + "://" + hostPart, nil
}
