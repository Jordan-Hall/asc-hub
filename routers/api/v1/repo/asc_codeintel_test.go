// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"forgejo.org/models/unittest"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	api_context "forgejo.org/services/context"
	"forgejo.org/services/contexttest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const ascCIAwsKey = "AKIAIOSFODNN7EXAMPLE"

type ascCIStream struct {
	grpc.ClientStream
	msgs []*ascv1.SearchResponse
}

func (s *ascCIStream) Recv() (*ascv1.SearchResponse, error) {
	if len(s.msgs) == 0 {
		return nil, io.EOF
	}
	m := s.msgs[0]
	s.msgs = s.msgs[1:]
	return m, nil
}

type ascCIFake struct {
	ascv1.CodeIntelClient
	search   *ascv1.SearchResponse
	symbols  *ascv1.SymbolsResponse
	semantic *ascv1.SemanticResponse
	err      error
	calls    int
	lastReq  any
}

func (f *ascCIFake) Search(_ context.Context, in *ascv1.SearchRequest, _ ...grpc.CallOption) (grpc.ServerStreamingClient[ascv1.SearchResponse], error) {
	f.calls++
	f.lastReq = in
	if f.err != nil {
		return nil, f.err
	}
	return &ascCIStream{msgs: []*ascv1.SearchResponse{f.search}}, nil
}

func (f *ascCIFake) Symbols(_ context.Context, in *ascv1.SymbolsRequest, _ ...grpc.CallOption) (*ascv1.SymbolsResponse, error) {
	f.calls++
	f.lastReq = in
	return f.symbols, f.err
}

func (f *ascCIFake) Semantic(_ context.Context, in *ascv1.SemanticRequest, _ ...grpc.CallOption) (*ascv1.SemanticResponse, error) {
	f.calls++
	f.lastReq = in
	return f.semantic, f.err
}

// ascCICall runs a handler against repository 1 with the given Authorization header ("" for none).
func ascCICall(t *testing.T, h func(*api_context.APIContext), target, auth string) (int, string) {
	t.Helper()
	ctx, resp := contexttest.MockAPIContext(t, target)
	contexttest.LoadRepo(t, ctx, 1)
	if auth != "" {
		ctx.Req.Header.Set("Authorization", auth)
	}
	h(ctx)
	return resp.Code, resp.Body.String()
}

func TestAscCodeintelAPI(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	t.Cleanup(func() { SetAscCodeIntel(nil) })

	t.Run("search maps the request and the answer", func(t *testing.T) {
		f := &ascCIFake{search: &ascv1.SearchResponse{
			Scope: &ascv1.ResolvedScope{Changeset: []byte{1, 2}, Generation: 7, Coverage: 1},
			Files: []*ascv1.FileHit{{
				Path: []byte("a/b.go"), Lang: "Go",
				Matches: []*ascv1.LineMatch{{Line: 3, Cols: []uint32{1}, Text: []byte("func Foo()\x1b[31m‮"), Before: [][]byte{[]byte("x")}}},
			}},
			NextPageToken: "tok123",
		}}
		SetAscCodeIntel(f)
		code, body := ascCICall(t, AscSearch, "user2/repo1/asc/search?q=Foo&mode=word&lang=Go&path=a&limit=5", "Bearer user-token-1")
		require.Equal(t, http.StatusOK, code, body)
		req := f.lastReq.(*ascv1.SearchRequest)
		assert.Equal(t, "Foo", req.GetPattern())
		assert.Equal(t, ascv1.SearchMode_SEARCH_MODE_WORD, req.GetMode())
		assert.Equal(t, "user2/repo1", req.GetScope().GetRepo().GetName())
		assert.Equal(t, "master", req.GetScope().GetRev())
		assert.Equal(t, []string{"Go"}, req.GetScope().GetLangs())
		assert.EqualValues(t, 5, req.GetBudget().GetMaxResults())
		assert.Contains(t, body, `"path":"a/b.go"`)
		assert.Contains(t, body, `"text":"func Foo()[31m"`) // control and bidi characters are gone
		assert.Contains(t, body, `"next_page_token":"tok123"`)
		assert.Contains(t, body, `"changeset":"0102"`)
	})

	// An AWS key in a hit the server marked redacted never reaches the JSON, even if the message carried
	// it. Without the redacted check in ascCIHit the key is in the body.
	t.Run("redacted hit and chunk carry no text", func(t *testing.T) {
		f := &ascCIFake{
			search: &ascv1.SearchResponse{Files: []*ascv1.FileHit{{
				Path: []byte("conf.go"), Redacted: true,
				Matches: []*ascv1.LineMatch{{Line: 7, Cols: []uint32{3}, Text: []byte("key = " + ascCIAwsKey), After: [][]byte{[]byte(ascCIAwsKey)}}},
			}}},
			semantic: &ascv1.SemanticResponse{Chunks: []*ascv1.Chunk{{
				Path: []byte("conf.go"), Redacted: true, Text: []byte("key = " + ascCIAwsKey),
			}}},
		}
		SetAscCodeIntel(f)
		code, body := ascCICall(t, AscSearch, "user2/repo1/asc/search?q=key", "token user-token-1")
		require.Equal(t, http.StatusOK, code)
		assert.NotContains(t, body, "AKIA")
		assert.Contains(t, body, `"redacted":true`)
		assert.Contains(t, body, `"line":7`)

		code, body = ascCICall(t, AscSemantic, "user2/repo1/asc/semantic?q=key", "Bearer user-token-1")
		require.Equal(t, http.StatusOK, code)
		assert.NotContains(t, body, "AKIA")
		assert.Contains(t, body, `"redacted":true`)
	})

	// Without the caller's token nothing is sent to asc: the route never falls back to the internal token.
	t.Run("no token is refused before the call", func(t *testing.T) {
		f := &ascCIFake{search: &ascv1.SearchResponse{}}
		SetAscCodeIntel(f)
		for _, auth := range []string{"", "Bearer ", "Bearer bad token", "Negotiate abc"} {
			code, _ := ascCICall(t, AscSearch, "user2/repo1/asc/search?q=Foo", auth)
			assert.Equal(t, http.StatusUnauthorized, code, auth)
		}
		assert.Zero(t, f.calls)
	})

	t.Run("bad input is refused before the call", func(t *testing.T) {
		f := &ascCIFake{search: &ascv1.SearchResponse{}, symbols: &ascv1.SymbolsResponse{}}
		SetAscCodeIntel(f)
		for _, target := range []string{
			"user2/repo1/asc/search",
			"user2/repo1/asc/search?q=" + strings.Repeat("x", ascCIMaxQueryBytes+1),
			"user2/repo1/asc/search?q=x&mode=nope",
			"user2/repo1/asc/search?q=x&rev=--upload-pack%3Dx",
			"user2/repo1/asc/search?q=x&rev=a%00b",
			"user2/repo1/asc/search?q=x&page_token=a%20b",
			"user2/repo1/asc/search?q=x&context=99",
			"user2/repo1/asc/symbols?op=def",
			"user2/repo1/asc/symbols?op=bogus&name=x",
			"user2/repo1/asc/symbols?op=outline",
		} {
			h := AscSearch
			if strings.Contains(target, "/symbols") {
				h = AscSymbols
			}
			code, _ := ascCICall(t, h, target, "Bearer user-token-1")
			assert.Equal(t, http.StatusUnprocessableEntity, code, target)
		}
		assert.Zero(t, f.calls)
	})

	t.Run("symbols", func(t *testing.T) {
		f := &ascCIFake{symbols: &ascv1.SymbolsResponse{Symbols: []*ascv1.Symbol{
			{Name: "Foo", Kind: "function", IsDef: true, Path: []byte("a.go"), Line: 2, Confidence: "name_match"},
		}}}
		SetAscCodeIntel(f)
		code, body := ascCICall(t, AscSymbols, "user2/repo1/asc/symbols?op=refs&name=Foo&rev=feature", "Bearer user-token-1")
		require.Equal(t, http.StatusOK, code, body)
		req := f.lastReq.(*ascv1.SymbolsRequest)
		assert.Equal(t, ascv1.SymbolOp_SYMBOL_OP_REFS, req.GetOp())
		assert.Equal(t, "feature", req.GetScope().GetRev())
		assert.Contains(t, body, `"confidence":"name_match"`)
	})

	t.Run("errors do not leak the server text", func(t *testing.T) {
		f := &ascCIFake{err: status.Error(codes.PermissionDenied, "secret detail"), search: &ascv1.SearchResponse{}}
		SetAscCodeIntel(f)
		code, body := ascCICall(t, AscSearch, "user2/repo1/asc/search?q=x", "Bearer user-token-1")
		assert.Equal(t, http.StatusNotFound, code)
		assert.NotContains(t, body, "secret detail")

		f.err = status.Error(codes.Internal, "stack trace here")
		code, body = ascCICall(t, AscSearch, "user2/repo1/asc/search?q=x", "Bearer user-token-1")
		assert.Equal(t, http.StatusBadGateway, code)
		assert.NotContains(t, body, "stack trace")
	})

	t.Run("switched off", func(t *testing.T) {
		SetAscCodeIntel(nil)
		code, _ := ascCICall(t, AscSearch, "user2/repo1/asc/search?q=x", "Bearer user-token-1")
		assert.Equal(t, http.StatusNotImplemented, code)
	})
}

func TestAscCodeintelToken(t *testing.T) {
	h := func(v string) *http.Request {
		r, _ := http.NewRequest(http.MethodGet, "/", nil)
		r.Header.Set("Authorization", v)
		return r
	}
	assert.Equal(t, "abc", ascCIToken(h("Bearer abc")))
	assert.Equal(t, "abc", ascCIToken(h("token abc")))
	assert.Equal(t, "pw", ascCIToken(h("Basic dXNlcjpwdw==")))
	assert.Empty(t, ascCIToken(h("Basic !!!")))
	assert.Empty(t, ascCIToken(h("abc")))
}
