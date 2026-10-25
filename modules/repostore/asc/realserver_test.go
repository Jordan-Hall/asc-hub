// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package asc_test

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"forgejo.org/modules/ascclient"
	"forgejo.org/modules/repostore"
	"forgejo.org/modules/repostore/asc"

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

// The standalone `asc serve` authorises with its deny-all default. Every write through the Writer must
// then fail closed over the real wire: an error that is not a success, not "exists" and not a missing
// repository, with no token in the text, and with no fallback to anything else.
func TestRealAscServeRefusesEveryWriteByDefault(t *testing.T) {
	addr := startAscServe(t)
	const token = "internal-token-for-tests"
	client, err := ascclient.New(ascclient.Config{
		Target: addr, InternalToken: token,
		Retry: ascclient.RetryConfig{Base: time.Millisecond, Cap: 5 * time.Millisecond},
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = client.Close() })
	w, err := asc.New(client.Repo(), asc.Options{})
	require.NoError(t, err)

	repo := ref("owner", "repo")
	check := func(name string, err error) {
		t.Helper()
		require.Error(t, err, name)
		assert.NotContains(t, err.Error(), token, name)
		assert.NotErrorIs(t, err, repostore.ErrExists, name)
		var ce *asc.Error
		assert.ErrorAs(t, err, &ce, "%s: the server answered over the wire: %v", name, err)
		t.Logf("%s against the real server: %v", name, err)
	}
	check("Init", w.Init(t.Context(), repostore.InitRequest{Repo: repo, DefaultBranch: "master"}))
	_, err = w.Size(t.Context(), repo)
	check("Size", err)
	_, err = w.CreateCommit(t.Context(), repostore.CommitRequest{
		Repo: repo, Ops: []repostore.FileOp{{Kind: repostore.OpPut, Path: "a", Content: strings.NewReader("a")}},
		Meta: repostore.CommitMeta{Committer: sig("Bob"), Message: "m"},
	})
	check("CreateCommit", err)
}
