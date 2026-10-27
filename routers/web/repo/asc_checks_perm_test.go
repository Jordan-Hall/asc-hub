// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	"net"
	"net/http"
	"sync/atomic"
	"testing"

	perm_model "forgejo.org/models/perm"
	"forgejo.org/models/unit"
	"forgejo.org/models/unittest"
	"forgejo.org/modules/templates"
	"forgejo.org/services/context"
	"forgejo.org/services/contexttest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// countingServer is an asc serve that counts every call it receives and refuses it.
func countingServer(t *testing.T) *atomic.Int64 {
	t.Helper()
	var calls atomic.Int64
	srv := grpc.NewServer(grpc.UnknownServiceHandler(func(any, grpc.ServerStream) error {
		calls.Add(1)
		return status.Error(codes.Unimplemented, "counted")
	}))
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	go func() { _ = srv.Serve(l) }()
	t.Cleanup(srv.Stop)
	useAsc(t, l.Addr().String())
	return &calls
}

// TestAscChecksNeedActionsRead: the checks pages show CI failure groups, occurrences and runs, so a reader of the
// code who may not read the Actions of the repository gets 404 and nothing is loaded from asc.
func TestAscChecksNeedActionsRead(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	setStorageBackend(t, 1, "asc")
	calls := countingServer(t)

	setPerm := func(actions perm_model.AccessMode) func(*context.Context) {
		return func(ctx *context.Context) {
			ctx.Repo.Permission.AccessMode = perm_model.AccessModeRead
			ctx.Repo.Permission.UnitsMode = map[unit.Type]perm_model.AccessMode{
				unit.TypeCode:         perm_model.AccessModeRead,
				unit.TypePullRequests: perm_model.AccessModeRead,
				unit.TypeActions:      actions,
			}
		}
	}
	handlers := map[string]struct {
		path   string
		params map[string]string
		run    func(*context.Context)
	}{
		"checks":      {"/user2/repo1/asc/checks", nil, AscChecks},
		"group":       {"/user2/repo1/asc/checks/groups/3", map[string]string{":id": "3"}, AscCheckGroup},
		"pull checks": {"/user2/repo1/pulls/2/asc/checks", map[string]string{":index": "2"}, AscPullChecks},
	}
	serve := func(t *testing.T, name string, actions perm_model.AccessMode) int {
		t.Helper()
		tc := handlers[name]
		ctx, resp := contexttest.MockContext(t, tc.path)
		ctx.Render = templates.HTMLRenderer()
		contexttest.LoadRepo(t, ctx, 1)
		setPerm(actions)(ctx)
		for k, v := range tc.params {
			ctx.SetParams(k, v)
		}
		tc.run(ctx)
		return resp.Code
	}
	for name := range handlers {
		t.Run(name+" without actions read", func(t *testing.T) {
			assert.Equal(t, http.StatusNotFound, serve(t, name, perm_model.AccessModeNone))
			assert.Zero(t, calls.Load(), "nothing is asked of asc")
		})
	}
	t.Run("with actions read the page is served", func(t *testing.T) {
		assert.NotEqual(t, http.StatusNotFound, serve(t, "checks", perm_model.AccessModeRead))
	})
}
