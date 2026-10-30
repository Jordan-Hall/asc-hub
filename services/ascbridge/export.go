// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package ascbridge

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"forgejo.org/modules/setting"
	"forgejo.org/modules/util"
)

// ExportTimeout bounds one `asc export` run, and the clone or fetch before it.
const ExportTimeout = 30 * time.Minute

// workspaceReady is the file that marks a workspace whose clone finished. A workspace without it is made again.
const workspaceReady = ".forgejo-asc-bridge-ready"

// MaxOutputBytes bounds the output of an asc child that an error repeats.
const MaxOutputBytes = 2048

// CLIExporter builds a bridge with the `asc` binary on this host. It keeps one asc workspace per repository
// below Workspaces, brings it up to date with the server (`asc clone` the first time, `asc fetch` after), and runs
// `asc export --git <bridge>` in it. The child gets a minimal environment: PATH, a private HOME and ASC_HOME inside
// the workspace, and ASC_TOKEN set to the internal token, which asc sends to the origin of the remote only.
type CLIExporter struct {
	// Binary is the asc executable ([asc] ASC_BINARY).
	Binary string
	// Workspaces holds the workspaces ([asc] BRIDGE_WORKSPACES; default <AppDataPath>/asc-bridge).
	Workspaces string
	// ServerURL is asc serve's base URL.
	ServerURL string
	// Token is the internal token.
	Token string
	// Timeout bounds a run (default ExportTimeout).
	Timeout time.Duration
}

// NewCLIExporter builds the exporter from the [asc] settings.
func NewCLIExporter() *CLIExporter {
	ws := setting.Asc.BridgeWorkspaces
	if ws == "" {
		ws = filepath.Join(setting.AppDataPath, "asc-bridge")
	}
	return &CLIExporter{Binary: setting.Asc.AscBinary, Workspaces: ws, ServerURL: setting.Asc.ServerURL, Token: setting.InternalToken}
}

// Export implements Exporter.
func (e *CLIExporter) Export(ctx context.Context, repoName, bridgeDir string) error {
	if e.Token == "" {
		return errors.New("ascbridge: the internal token is not set")
	}
	owner, name, ok := strings.Cut(repoName, "/")
	if !ok || !safeElement(owner) || !safeElement(name) {
		return fmt.Errorf("ascbridge: %q is not a repository name", repoName)
	}
	timeout := e.Timeout
	if timeout <= 0 {
		timeout = ExportTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ws := filepath.Join(e.Workspaces, owner, name)
	if err := os.MkdirAll(filepath.Dir(ws), 0o750); err != nil {
		return fmt.Errorf("ascbridge: workspace directory: %w", err)
	}
	// One private home for every run of this repository. The workspace the clone makes points at the caches in the
	// asc home it was made with, so a fetch or an export with another one cannot find its repository ("not a git
	// repository"). The home is not in the workspace, which does not exist yet when the clone runs.
	home := filepath.Join(e.Workspaces, ".home", owner, name)
	if err := os.MkdirAll(home, 0o750); err != nil {
		return fmt.Errorf("ascbridge: workspace home: %w", err)
	}
	ready := filepath.Join(ws, workspaceReady)
	if _, err := os.Stat(ready); err != nil {
		if !os.IsNotExist(err) {
			return fmt.Errorf("ascbridge: workspace: %w", err)
		}
		// A half-made workspace of an earlier failed clone is removed before the clone starts again.
		if err := util.RemoveAll(ws); err != nil {
			return fmt.Errorf("ascbridge: workspace: %w", err)
		}
		if err := e.run(ctx, filepath.Dir(ws), home, "clone", "--", strings.TrimRight(e.ServerURL, "/")+"/"+repoName, name); err != nil {
			return err
		}
		if err := os.WriteFile(ready, nil, 0o600); err != nil {
			return fmt.Errorf("ascbridge: workspace: %w", err)
		}
	} else if err := e.run(ctx, ws, home, "fetch"); err != nil {
		return err
	}
	return e.run(ctx, ws, home, "export", "--git", bridgeDir, "--refs", "refs/heads/*", "--refs", "refs/tags/*", "--refs", "refs/pull/*")
}

// safeElement accepts the characters of owner and repository names, and not a path element that climbs or one that
// starts with '-', which a command line would read as an option. The clone also puts "--" before its positionals.
func safeElement(s string) bool {
	if s == "" || s == "." || s == ".." || s[0] == '-' {
		return false
	}
	for i := range len(s) {
		switch c := s[i]; {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '-', c == '_', c == '.':
		default:
			return false
		}
	}
	return true
}

func (e *CLIExporter) run(ctx context.Context, dir, home string, args ...string) error {
	cmd := exec.CommandContext(ctx, e.Binary, args...)
	cmd.Dir = dir
	cmd.Env = []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + home,
		"ASC_HOME=" + filepath.Join(home, ".asc-home"),
		"ASC_TOKEN=" + e.Token,
	}
	if loopbackHTTP(e.ServerURL) {
		// The hub talks to asc serve over plain http on the same host (as its own client does); asc refuses that
		// unless it is told the address is the loopback.
		cmd.Env = append(cmd.Env, "ASC_ALLOW_LOOPBACK_HTTP=1")
	}
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	if err := cmd.Run(); err != nil {
		text := strings.ReplaceAll(out.String(), e.Token, "***")
		if len(text) > MaxOutputBytes {
			text = text[:MaxOutputBytes] + "..."
		}
		return fmt.Errorf("ascbridge: asc %s: %w: %s", args[0], err, strings.TrimSpace(text))
	}
	return nil
}

// loopbackHTTP reports whether rawURL is plain http to a loopback address.
func loopbackHTTP(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil || u.Scheme != "http" {
		return false
	}
	host := u.Hostname()
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
