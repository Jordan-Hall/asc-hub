// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package user

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"forgejo.org/models/unittest"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/test"

	ascv1 "forgejo.org/modules/ascproto/ascv1"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
)

// workRenderWrite stores one rendered page for the design comparison when ASC_RENDER_DIR is set.
func workRenderWrite(t *testing.T, name string, body []byte) {
	t.Helper()
	dir := os.Getenv("ASC_RENDER_DIR")
	if dir == "" {
		return
	}
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, name+".html"), body, 0o644))
}

// TestWorkRenderDrafts renders the drafts page in the states of its boards: with drafts, none and unavailable.
func TestWorkRenderDrafts(t *testing.T) {
	if os.Getenv("ASC_RENDER_DIR") == "" {
		t.Skip("set ASC_RENDER_DIR to write the rendered pages")
	}
	require.NoError(t, unittest.PrepareTestDatabase())
	defer test.MockVariableValue(&setting.Asc.Enabled, true)()
	setAscBackend(t, 1, "asc")
	setAscBackend(t, 2, "asc")
	now := time.Now()
	change := func(b byte) []byte { return []byte{b, b, b, b, b, b, b, b} }
	cloud := &fakeCloud{heads: []*ascv1.CloudHead{
		{Cs: change(1), Change: change(0xa1), Labels: []string{"add-rate-limit-headers", "wip"}, Added: now.Add(-4 * time.Minute).Unix()},
		{Cs: change(2), Change: change(0xb2), Labels: []string{"spike/streaming-exports"}, Added: now.Add(-26 * time.Hour).Unix()},
		{Cs: change(3), Labels: []string{"docs-failover-order"}, Added: now.Add(-3 * 24 * time.Hour).Unix()},
	}}
	useFakeCloud(t, cloud)

	ctx, body := ascRenderContext(t, "/-/asc/drafts", 2)
	AscDrafts(ctx)
	workRenderWrite(t, "drafts", body())

	cloud.heads = nil
	ctx, body = ascRenderContext(t, "/-/asc/drafts", 2)
	AscDrafts(ctx)
	workRenderWrite(t, "drafts-empty", body())

	ascDraftsConnMu.Lock()
	ascDraftsConnVal = nil
	ascDraftsNewConn = func() (grpc.ClientConnInterface, error) { return nil, errors.New("down") }
	ascDraftsConnMu.Unlock()
	ctx, body = ascRenderContext(t, "/-/asc/drafts", 2)
	AscDrafts(ctx)
	workRenderWrite(t, "drafts-error", body())
}
