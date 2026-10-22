// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package ascclient

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	ascv1 "forgejo.org/modules/ascproto/ascv1"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

// startAscServe runs the real `asc serve` from ASC_SERVER_BIN (built by
// `cargo build -p asc --release --no-default-features --features serve`) and returns its address.
// The test is skipped when the variable is unset or the binary is missing, so the unit tests
// above stay hermetic. The standalone binary authorises with its deny-all default, which is
// what these tests rely on: they prove wire compatibility and fail-closed status mapping.
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

func TestAscServeDeniesByDefaultAndSpeaksOurWireFormat(t *testing.T) {
	addr := startAscServe(t)
	c, err := New(Config{Target: addr, InternalToken: "internal", Retry: RetryConfig{Base: time.Millisecond, Cap: 5 * time.Millisecond}})
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Close() })

	deniedOrOK := func(err error) {
		t.Helper()
		if err == nil {
			return
		}
		assert.Contains(t, []codes.Code{codes.Unauthenticated, codes.PermissionDenied, codes.NotFound}, status.Code(err), "%v", err)
	}

	resp, err := c.Repo().Version(WithSystem(t.Context()), &ascv1.VersionRequest{})
	deniedOrOK(err)
	if err == nil {
		assert.NotEmpty(t, resp.GetServerVersion())
	}

	// Our bytes decode on the server and its status decodes here: a user token with the
	// deny-all authoriser is refused, never served and never retried into success.
	ctx, err := WithUserToken(t.Context(), "some-user-token")
	require.NoError(t, err)
	_, err = c.HasObjects(ctx, "owner/repo", []*ascv1.ObjKey{{Kind: ascv1.ObjKind_OBJ_KIND_FILE, Id: make([]byte, 32)}})
	require.Error(t, err)
	deniedOrOK(err)

	_, err = c.GetObjects(ctx, "owner/repo", []*ascv1.ObjKey{{Kind: ascv1.ObjKind_OBJ_KIND_FILE, Id: make([]byte, 32)}})
	require.Error(t, err)
	deniedOrOK(err)

	obj, err := c.NewObject(ascv1.ObjKind_OBJ_KIND_FILE, []byte("hello"))
	require.NoError(t, err)
	_, _, err = c.PutObjects(ctx, "owner/repo", []*ascv1.Object{obj})
	require.Error(t, err)
	deniedOrOK(err)
}
