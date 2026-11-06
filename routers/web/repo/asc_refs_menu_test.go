// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	gocontext "context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"forgejo.org/models/unittest"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/test"
	"forgejo.org/services/ascsync"
	"forgejo.org/services/contexttest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestAscRefsMenuItems(t *testing.T) {
	t.Run("shared refs, stacks, tags, each once", func(t *testing.T) {
		got := ascRefsMenuItems([]string{"main", "release/2.18", ""}, []string{"retry", "main"}, []string{"v2.18.0", "retry"}, []string{"ignored"}, false)
		assert.Equal(t, []ascRefItem{
			{Name: "main", Kind: ascRefShared},
			{Name: "release/2.18", Kind: ascRefShared},
			{Name: "retry", Kind: ascRefStack},
			{Name: "v2.18.0", Kind: ascRefTag},
		}, got)
	})

	t.Run("branchless lists asc's shared refs instead of the branches", func(t *testing.T) {
		got := ascRefsMenuItems([]string{"leftover"}, []string{"retry"}, nil, []string{"refs/heads/main", "release/2.18"}, true)
		assert.Equal(t, []ascRefItem{
			{Name: "main", Kind: ascRefShared},
			{Name: "release/2.18", Kind: ascRefShared},
			{Name: "retry", Kind: ascRefStack},
		}, got)
	})

	t.Run("each kind is capped", func(t *testing.T) {
		many := make([]string, ascCompareMaxRefs+5)
		for i := range many {
			many[i] = fmt.Sprintf("t%d", i)
		}
		got := ascRefsMenuItems(nil, nil, many, nil, false)
		assert.Len(t, got, ascCompareMaxRefs)
	})
}

type fakeRefsConfig struct {
	ascv1.RepoClient
	resp *ascv1.RepoConfigResponse
	err  error
}

func (f *fakeRefsConfig) RepoConfig(gocontext.Context, *ascv1.RepoConfigRequest, ...grpc.CallOption) (*ascv1.RepoConfigResponse, error) {
	return f.resp, f.err
}

func getAscRefsMenu(t *testing.T) (int, ascRefsMenu, http.Header) {
	t.Helper()
	ctx, resp := contexttest.MockContext(t, "/user2/repo1/asc/refs")
	contexttest.LoadUser(t, ctx, 2)
	contexttest.LoadRepo(t, ctx, 1)
	AscRefsMenu(ctx)
	var m ascRefsMenu
	if resp.Code == http.StatusOK {
		require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &m), resp.Body.String())
	}
	return resp.Code, m, resp.Header()
}

func TestAscRefsMenu(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	setStorageBackend(t, 1, "asc")
	defer test.MockVariableValue(&setting.Asc.Enabled, true)()
	f := &fakeRefsConfig{resp: &ascv1.RepoConfigResponse{Branchless: true, SharedRefs: []string{"refs/heads/main"}}}
	defer ascsync.UseClient(f)()

	t.Run("branchless: asc's shared refs and the tags", func(t *testing.T) {
		code, m, h := getAscRefsMenu(t)
		require.Equal(t, http.StatusOK, code)
		assert.Equal(t, "no-store", h.Get("Cache-Control"))
		assert.True(t, m.Branchless)
		require.NotEmpty(t, m.Refs)
		assert.Equal(t, ascRefItem{Name: "main", Kind: ascRefShared}, m.Refs[0])
		for _, r := range m.Refs[1:] {
			assert.NotEqual(t, ascRefShared, r.Kind, "only asc's shared refs: %v", r)
		}
		assert.Contains(t, m.Refs, ascRefItem{Name: "v1.1", Kind: ascRefTag})
	})

	t.Run("without asc the hub's branches are listed", func(t *testing.T) {
		f.resp, f.err = nil, status.Error(codes.Unavailable, "down")
		defer func() {
			f.resp, f.err = &ascv1.RepoConfigResponse{Branchless: true, SharedRefs: []string{"refs/heads/main"}}, nil
		}()
		code, m, _ := getAscRefsMenu(t)
		require.Equal(t, http.StatusOK, code)
		assert.False(t, m.Branchless)
		assert.Contains(t, m.Refs, ascRefItem{Name: "master", Kind: ascRefShared})
		assert.Contains(t, m.Refs, ascRefItem{Name: "branch2", Kind: ascRefStack}, "an open pull head is a stack, listed once")
		assert.NotContains(t, m.Refs, ascRefItem{Name: "branch2", Kind: ascRefShared})
	})

	t.Run("not served outside asc", func(t *testing.T) {
		setStorageBackend(t, 1, "git")
		defer setStorageBackend(t, 1, "asc")
		code, _, _ := getAscRefsMenu(t)
		assert.Equal(t, http.StatusNotFound, code)
	})

	t.Run("not served when asc is off", func(t *testing.T) {
		defer test.MockVariableValue(&setting.Asc.Enabled, false)()
		code, _, _ := getAscRefsMenu(t)
		assert.Equal(t, http.StatusNotFound, code)
	})
}
