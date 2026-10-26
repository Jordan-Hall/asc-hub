// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package ascsearch

import (
	"context"
	"fmt"
	"net"
	"sync"
	"testing"

	"forgejo.org/modules/ascclient"
	ascv1 "forgejo.org/modules/ascproto/ascv1"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

const testInternalToken = "internal-test-token"

// fakeAsc is an asc serve. A nil handler answers Unimplemented, like a server without the feature.
type fakeAsc struct {
	mu    sync.Mutex
	auth  []string
	cross []*ascv1.CrossSearchRequest
	look  []*ascv1.LookupSymbolRequest
	panel []*ascv1.SymbolPanelRequest
	srch  []*ascv1.SearchRequest
	syms  []*ascv1.SymbolsRequest
	sems  []*ascv1.SemanticRequest
	idx   []*ascv1.IndexStatusRequest

	crossSearch func(*ascv1.CrossSearchRequest) ([]*ascv1.CrossSearchResponse, error)
	lookup      func(*ascv1.LookupSymbolRequest) (*ascv1.LookupSymbolResponse, error)
	symbolPanel func(*ascv1.SymbolPanelRequest) (*ascv1.SymbolPanelResponse, error)
	search      func(*ascv1.SearchRequest) ([]*ascv1.SearchResponse, error)
	symbols     func(*ascv1.SymbolsRequest) (*ascv1.SymbolsResponse, error)
	semantic    func(*ascv1.SemanticRequest) (*ascv1.SemanticResponse, error)
	index       func(*ascv1.IndexStatusRequest) (*ascv1.IndexStatusResponse, error)
}

func (f *fakeAsc) note(ctx context.Context) {
	md, _ := metadata.FromIncomingContext(ctx)
	f.auth = append(f.auth, fmt.Sprint(md.Get("authorization")))
}

func (f *fakeAsc) Search(req *ascv1.CrossSearchRequest, s grpc.ServerStreamingServer[ascv1.CrossSearchResponse]) error {
	f.mu.Lock()
	f.cross = append(f.cross, req)
	f.note(s.Context())
	fn := f.crossSearch
	f.mu.Unlock()
	if fn == nil {
		return status.Error(codes.Unimplemented, "no cross search")
	}
	msgs, err := fn(req)
	if err != nil {
		return err
	}
	for _, m := range msgs {
		if err := s.Send(m); err != nil {
			return err
		}
	}
	return nil
}

func (f *fakeAsc) LookupSymbol(ctx context.Context, req *ascv1.LookupSymbolRequest) (*ascv1.LookupSymbolResponse, error) {
	f.mu.Lock()
	f.look = append(f.look, req)
	f.note(ctx)
	fn := f.lookup
	f.mu.Unlock()
	if fn == nil {
		return nil, status.Error(codes.Unimplemented, "no lookup")
	}
	return fn(req)
}

func (f *fakeAsc) SymbolPanel(ctx context.Context, req *ascv1.SymbolPanelRequest) (*ascv1.SymbolPanelResponse, error) {
	f.mu.Lock()
	f.panel = append(f.panel, req)
	f.note(ctx)
	fn := f.symbolPanel
	f.mu.Unlock()
	if fn == nil {
		return nil, status.Error(codes.Unimplemented, "no panel")
	}
	return fn(req)
}

// codeIntel is the CodeIntel side; it is a separate type because both services have a Search method.
type codeIntel struct {
	ascv1.UnimplementedCodeIntelServer
	f *fakeAsc
}

func (c codeIntel) Search(req *ascv1.SearchRequest, s grpc.ServerStreamingServer[ascv1.SearchResponse]) error {
	f := c.f
	f.mu.Lock()
	f.srch = append(f.srch, req)
	f.note(s.Context())
	fn := f.search
	f.mu.Unlock()
	if fn == nil {
		return status.Error(codes.Unimplemented, "no search")
	}
	msgs, err := fn(req)
	if err != nil {
		return err
	}
	for _, m := range msgs {
		if err := s.Send(m); err != nil {
			return err
		}
	}
	return nil
}

func (c codeIntel) Symbols(ctx context.Context, req *ascv1.SymbolsRequest) (*ascv1.SymbolsResponse, error) {
	f := c.f
	f.mu.Lock()
	f.syms = append(f.syms, req)
	f.note(ctx)
	fn := f.symbols
	f.mu.Unlock()
	if fn == nil {
		return nil, status.Error(codes.Unimplemented, "no symbols")
	}
	return fn(req)
}

func (c codeIntel) Semantic(ctx context.Context, req *ascv1.SemanticRequest) (*ascv1.SemanticResponse, error) {
	f := c.f
	f.mu.Lock()
	f.sems = append(f.sems, req)
	f.note(ctx)
	fn := f.semantic
	f.mu.Unlock()
	if fn == nil {
		return nil, status.Error(codes.Unimplemented, "no semantic")
	}
	return fn(req)
}

func (c codeIntel) IndexStatus(ctx context.Context, req *ascv1.IndexStatusRequest) (*ascv1.IndexStatusResponse, error) {
	f := c.f
	f.mu.Lock()
	f.idx = append(f.idx, req)
	f.note(ctx)
	fn := f.index
	f.mu.Unlock()
	if fn == nil {
		return nil, status.Error(codes.Unimplemented, "no index status")
	}
	return fn(req)
}

// startFake serves f and returns a Service bound to it.
func startFake(t *testing.T, f *fakeAsc) *Service {
	t.Helper()
	srv := grpc.NewServer()
	ascv1.RegisterCrossSearchServer(srv, crossOnly{f: f})
	ascv1.RegisterCodeIntelServer(srv, codeIntel{f: f})
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	go func() { _ = srv.Serve(l) }()
	t.Cleanup(srv.Stop)
	c, err := ascclient.New(ascclient.Config{Target: l.Addr().String(), InternalToken: testInternalToken, Retry: ascclient.RetryConfig{MaxAttempts: 1}})
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Close() })
	return New(c)
}

// crossOnly exposes only the CrossSearch methods of fakeAsc.
type crossOnly struct {
	ascv1.UnimplementedCrossSearchServer
	f *fakeAsc
}

func (c crossOnly) Search(req *ascv1.CrossSearchRequest, s grpc.ServerStreamingServer[ascv1.CrossSearchResponse]) error {
	return c.f.Search(req, s)
}

func (c crossOnly) LookupSymbol(ctx context.Context, req *ascv1.LookupSymbolRequest) (*ascv1.LookupSymbolResponse, error) {
	return c.f.LookupSymbol(ctx, req)
}

func (c crossOnly) SymbolPanel(ctx context.Context, req *ascv1.SymbolPanelRequest) (*ascv1.SymbolPanelResponse, error) {
	return c.f.SymbolPanel(ctx, req)
}

func sys(t *testing.T) context.Context {
	t.Helper()
	return ascclient.WithSystem(t.Context())
}

func userCtx(t *testing.T, token string) context.Context {
	t.Helper()
	ctx, err := ascclient.WithUserToken(t.Context(), token)
	require.NoError(t, err)
	return ctx
}

func repoN(i int) Repo {
	return Repo{ID: int64(i), Name: fmt.Sprintf("o/r%d", i), FullName: fmt.Sprintf("o/r%d", i), Branch: "main", Rev: "refs/heads/main", Updated: int64(1000 - i)}
}

func reposN(n int) []Repo {
	out := make([]Repo, n)
	for i := range out {
		out[i] = repoN(i + 1)
	}
	return out
}

func ref(name string) *ascv1.RepoRef { return &ascv1.RepoRef{Name: name} }

func hit(path, text string) *ascv1.FileHit {
	return &ascv1.FileHit{Path: []byte(path), Lang: "Go", Matches: []*ascv1.LineMatch{{Line: 3, Cols: []uint32{1}, Text: []byte(text)}}}
}
