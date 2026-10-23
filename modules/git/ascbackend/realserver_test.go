// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package ascbackend_test

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"forgejo.org/modules/ascclient"
	"forgejo.org/modules/git"
	"forgejo.org/modules/git/ascbackend"

	"github.com/stretchr/testify/require"
)

func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

// startAscServe runs the real `asc serve` from ASC_SERVER_BIN (built by
// `cargo build -p asc --release --no-default-features --features serve`) and returns its address. The test
// is skipped when the variable is unset or the binary cannot be started here.
func startAscServe(t *testing.T) string {
	t.Helper()
	bin := os.Getenv("ASC_SERVER_BIN")
	if bin == "" {
		t.Skip("ASC_SERVER_BIN is not set")
	}
	if _, err := os.Stat(bin); err != nil {
		t.Skipf("ASC_SERVER_BIN %q is not usable: %v", bin, err)
	}
	dir := t.TempDir()
	port, adminPort := freePort(t), freePort(t)
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "bucket"), 0o755))
	conf := filepath.Join(dir, "asc-serve.toml")
	require.NoError(t, os.WriteFile(conf, fmt.Appendf(nil,
		"listen = \"127.0.0.1:%d\"\ndb = \"sqlite:%s?mode=rwc\"\nstore = \"file://%s\"\n\n[admin]\nlisten = \"127.0.0.1:%d\"\n",
		port, filepath.ToSlash(filepath.Join(dir, "asc.db")), filepath.ToSlash(filepath.Join(dir, "bucket")), adminPort), 0o600))

	env := append(os.Environ(), "ASC_HOME="+filepath.Join(dir, "home"), "HOME="+dir)
	migrate := exec.CommandContext(t.Context(), bin, "serve", "migrate", "--config", conf)
	migrate.Env = env
	if out, err := migrate.CombinedOutput(); err != nil {
		t.Fatalf("asc serve migrate: %v\n%s", err, out)
	}
	ctx, cancel := context.WithCancel(t.Context())
	srv := exec.CommandContext(ctx, bin, "serve", "--config", conf)
	srv.Env = env
	require.NoError(t, srv.Start())
	t.Cleanup(func() { cancel(); _ = srv.Wait() })

	addr := fmt.Sprintf("127.0.0.1:%d", port)
	deadline := time.Now().Add(20 * time.Second)
	for {
		conn, err := net.DialTimeout("tcp", addr, 200*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			return addr
		}
		if time.Now().After(deadline) {
			t.Fatalf("asc serve did not listen on %s: %v", addr, err)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// The standalone `asc serve` authorises with its deny-all default. Every read through the backend must
// therefore fail, and must not read as a missing object or fall back to anything else.
func TestRealAscServeRefusesEveryReadByDefault(t *testing.T) {
	addr := startAscServe(t)
	client, err := ascclient.New(ascclient.Config{Target: addr, InternalToken: "internal", Retry: ascclient.RetryConfig{Base: time.Millisecond, Cap: 5 * time.Millisecond}})
	require.NoError(t, err)
	t.Cleanup(func() { _ = client.Close() })

	root := filepath.Join(t.TempDir(), "repos")
	bare := filepath.Join(root, "owner", "repo.git")
	require.NoError(t, os.MkdirAll(bare, 0o755))
	gitRun(t, bare, nil, "", "init", "-q", "--bare")
	repo, err := git.OpenRepositoryWithBackend(t.Context(), bare, "gitcli")
	require.NoError(t, err)
	t.Cleanup(func() { _ = repo.Close() })
	be, err := ascbackend.New(client.Repo(), "owner/repo", repo, ascbackend.Options{})
	require.NoError(t, err)

	id := git.MustIDFromString("0123456789012345678901234567890123456789")
	// An unreadable repository may answer like a missing one (SPEC section 15.1), so only the error is asserted.
	_, err = be.ReadCommit(t.Context(), id)
	require.Error(t, err)
	_, _, err = be.Resolve(t.Context(), "main")
	require.Error(t, err)
	_, err = be.ListRefs(t.Context(), git.RefQuery{})
	require.Error(t, err)
	_, _, err = be.OpenBlob(t.Context(), id, -1)
	require.Error(t, err)
}
