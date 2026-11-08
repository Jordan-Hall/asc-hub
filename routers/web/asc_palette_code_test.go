// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package web

import (
	"context"
	"net"
	"net/http"
	"sync"
	"testing"
	"time"

	"forgejo.org/models/db"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/ascclient"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/ascratelimit"
	"forgejo.org/modules/json"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/test"
	"forgejo.org/services/ascsearch"
	"forgejo.org/services/contexttest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// paletteCross is an asc serve that answers symbol lookups.
type paletteCross struct {
	ascv1.UnimplementedCrossSearchServer
	mu   sync.Mutex
	reqs []*ascv1.LookupSymbolRequest
	hits []*ascv1.SymbolHit
	err  error
	wait time.Duration
}

func (p *paletteCross) LookupSymbol(ctx context.Context, req *ascv1.LookupSymbolRequest) (*ascv1.LookupSymbolResponse, error) {
	p.mu.Lock()
	p.reqs = append(p.reqs, req)
	hits, err, wait := p.hits, p.err, p.wait
	p.mu.Unlock()
	if wait > 0 {
		select {
		case <-time.After(wait):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if err != nil {
		return nil, err
	}
	return &ascv1.LookupSymbolResponse{Hits: hits}, nil
}

func (p *paletteCross) calls() []*ascv1.LookupSymbolRequest {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]*ascv1.LookupSymbolRequest(nil), p.reqs...)
}

func usePaletteCross(t *testing.T, p *paletteCross) {
	t.Helper()
	srv := grpc.NewServer()
	ascv1.RegisterCrossSearchServer(srv, p)
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	go func() { _ = srv.Serve(l) }()
	t.Cleanup(srv.Stop)
	c, err := ascclient.New(ascclient.Config{Target: l.Addr().String(), InternalToken: "palette-internal-token", Retry: ascclient.RetryConfig{MaxAttempts: 1}})
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Close() })
	t.Cleanup(ascsearch.UseConn(c))
	t.Cleanup(test.MockVariableValue(&setting.Asc.Enabled, true))
}

func paletteCode(t *testing.T, q string, doer *user_model.User) (int, []AscPaletteItem, bool) {
	t.Helper()
	ctx, resp := contexttest.MockContext(t, "GET /-/asc/palette?q="+q)
	ctx.Req.Form = map[string][]string{"q": {q}}
	if doer != nil {
		contexttest.LoadUser(t, ctx, doer.ID)
		ctx.IsSigned = true
	}
	AscPalette(ctx)
	var out struct {
		Groups []AscPaletteGroup `json:"groups"`
	}
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &out))
	for _, g := range out.Groups {
		if g.Key == "code" {
			return resp.Code, g.Items, true
		}
	}
	return resp.Code, nil, false
}

func TestPaletteFindsCodeInReadableRepositories(t *testing.T) {
	unittest.PrepareTestEnv(t)
	ascPaletteLimiter = ascratelimit.New(1000, time.Second)
	ascPaletteUserLimiter = ascratelimit.New(1000, time.Second)
	_, err := db.GetEngine(db.DefaultContext).ID(1).Cols("storage_backend").Update(&repo_model.Repository{StorageBackend: "asc"})
	require.NoError(t, err)
	user2 := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2})

	p := &paletteCross{hits: []*ascv1.SymbolHit{
		{Repo: &ascv1.RepoRef{Name: "user2/repo1"}, Symbol: &ascv1.Symbol{Name: "RetryPolicy", Kind: "struct", IsDef: true, Path: []byte("src/retry policy.rs"), Line: 4, EndLine: 9, Confidence: "precise"}, Score: 1},
		{Repo: &ascv1.RepoRef{Name: "hidden/repo"}, Symbol: &ascv1.Symbol{Name: "retry", Kind: "fn", IsDef: true, Path: []byte("z.rs"), Line: 1, EndLine: 1, Confidence: "precise"}, Score: 1},
	}}
	usePaletteCross(t, p)

	code, items, ok := paletteCode(t, "retry", user2)
	assert.Equal(t, http.StatusOK, code)
	require.True(t, ok)
	require.Len(t, items, 1, "a repository Forgejo did not hand asc is dropped")
	assert.Equal(t, "RetryPolicy", items[0].Title)
	assert.Equal(t, "struct · user2/repo1 · src/retry policy.rs:4", items[0].Sub)
	assert.Equal(t, "/user2/repo1/src/branch/master/src/retry%20policy.rs#L4", items[0].Href)
	reqs := p.calls()
	require.Len(t, reqs, 1)
	assert.True(t, reqs[0].GetDefinitionsOnly())
	assert.Equal(t, ascv1.SymbolMatch_SYMBOL_MATCH_FUZZY, reqs[0].GetMatch())
	assert.Equal(t, "retry", reqs[0].GetName())
	assert.Contains(t, reqs[0].GetTarget().GetRepos(), "user2/repo1")
	assert.LessOrEqual(t, int(reqs[0].GetLimit()), ascPaletteEach)

	t.Run("asc off asks nothing", func(t *testing.T) {
		defer test.MockVariableValue(&setting.Asc.Enabled, false)()
		before := len(p.calls())
		_, _, ok := paletteCode(t, "retry", user2)
		assert.False(t, ok)
		assert.Len(t, p.calls(), before)
	})
	t.Run("the explore code page disabled asks nothing", func(t *testing.T) {
		defer test.MockVariableValue(&setting.Service.Explore.DisableCodePage, true)()
		before := len(p.calls())
		_, _, ok := paletteCode(t, "retry", user2)
		assert.False(t, ok)
		assert.Len(t, p.calls(), before)
	})
	t.Run("a failing asc leaves the group out", func(t *testing.T) {
		p.mu.Lock()
		p.err = status.Error(codes.Unavailable, "down")
		p.mu.Unlock()
		defer func() {
			p.mu.Lock()
			p.err = nil
			p.mu.Unlock()
		}()
		code, _, ok := paletteCode(t, "retry", user2)
		assert.Equal(t, http.StatusOK, code)
		assert.False(t, ok)
	})
	t.Run("a slow asc is not waited for", func(t *testing.T) {
		p.mu.Lock()
		p.wait = 5 * time.Second
		p.mu.Unlock()
		defer func() {
			p.mu.Lock()
			p.wait = 0
			p.mu.Unlock()
		}()
		start := time.Now()
		_, _, ok := paletteCode(t, "retry", user2)
		assert.False(t, ok)
		assert.Less(t, time.Since(start), 4*time.Second)
	})
	t.Run("busy hub leaves the group out", func(t *testing.T) {
		for range cap(ascPaletteCodeSlots) {
			ascPaletteCodeSlots <- struct{}{}
		}
		defer func() {
			for range cap(ascPaletteCodeSlots) {
				<-ascPaletteCodeSlots
			}
		}()
		before := len(p.calls())
		_, _, ok := paletteCode(t, "retry", user2)
		assert.False(t, ok)
		assert.Len(t, p.calls(), before)
	})
	t.Run("a visitor where explore needs a sign in", func(t *testing.T) {
		defer test.MockVariableValue(&setting.Service.RequireSignInView, true)()
		before := len(p.calls())
		_, _, ok := paletteCode(t, "retry", nil)
		assert.False(t, ok)
		assert.Len(t, p.calls(), before)
	})
}
