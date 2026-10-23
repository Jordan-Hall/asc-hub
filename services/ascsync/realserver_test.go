// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package ascsync

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

	"github.com/stretchr/testify/assert"
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
// `cargo build -p asc --release --no-default-features --features serve`) and returns its address. The test is
// skipped when the variable is unset or the binary cannot be started here.
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

// The standalone `asc serve` authorises with its deny-all default, so it answers nothing a push needs. Every
// path of this package must then fail closed over the real wire: an error, the bridge untouched, and no
// token in the text.
func TestRealAscServeRefusesEverythingByDefault(t *testing.T) {
	addr := startAscServe(t)
	fx := newFixture(t)
	const token = "internal-token-for-tests"
	client, err := ascclient.New(ascclient.Config{
		Target: addr, InternalToken: token,
		Retry: ascclient.RetryConfig{Base: time.Millisecond, Cap: 5 * time.Millisecond},
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = client.Close() })
	restore := UseClient(client.Repo())
	t.Cleanup(restore)

	c := fx.commit("one")
	err = ImportPush(t.Context(), ImportRequest{
		Repo: fx.repo, PusherID: 2,
		Refs: []PushRef{{Name: "refs/heads/main", Old: zeroID, New: c}},
	})
	require.Error(t, err)
	assert.NotContains(t, err.Error(), token)
	t.Logf("ImportPush against the real server: %v", err)
	_, answered := RejectMessage(err)
	assert.True(t, answered, "the server answered over the wire (permission denied), it was not a transport failure")

	err = SetPullHead(t.Context(), fx.repo, "refs/pull/1/head", c)
	require.Error(t, err)
	assert.NotContains(t, err.Error(), token)
	assert.Empty(t, fx.bridgeRef("refs/pull/1/head"), "the bridge is not written when asc refuses")

	require.Error(t, DeletePullHead(t.Context(), fx.repo, "refs/pull/1/head"))
}
