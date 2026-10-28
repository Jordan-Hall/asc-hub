// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	gocontext "context"
	"math"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"

	"forgejo.org/models/unittest"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/templates"
	"forgejo.org/services/contexttest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// fakeSearchServer is an asc serve that answers the CodeIntel calls of the search page. A nil field answers
// Unimplemented.
type fakeSearchServer struct {
	ascv1.UnimplementedCodeIntelServer
	mu       sync.Mutex
	searches []*ascv1.SearchRequest
	finds    []*ascv1.SymbolsRequest
	sems     []*ascv1.SemanticRequest
	statuses []*ascv1.IndexStatusRequest
	auth     [][]string

	search   func(*ascv1.SearchRequest) ([]*ascv1.SearchResponse, error)
	symbols  func(*ascv1.SymbolsRequest) (*ascv1.SymbolsResponse, error)
	semantic func(*ascv1.SemanticRequest) (*ascv1.SemanticResponse, error)
	index    func(*ascv1.IndexStatusRequest) (*ascv1.IndexStatusResponse, error)
}

func (f *fakeSearchServer) note(ctx gocontext.Context) {
	md, _ := metadata.FromIncomingContext(ctx)
	f.auth = append(f.auth, md.Get("authorization"))
}

func (f *fakeSearchServer) Search(req *ascv1.SearchRequest, stream grpc.ServerStreamingServer[ascv1.SearchResponse]) error {
	f.mu.Lock()
	f.searches = append(f.searches, req)
	f.note(stream.Context())
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
		if err := stream.Send(m); err != nil {
			return err
		}
	}
	return nil
}

func (f *fakeSearchServer) Symbols(ctx gocontext.Context, req *ascv1.SymbolsRequest) (*ascv1.SymbolsResponse, error) {
	f.mu.Lock()
	f.finds = append(f.finds, req)
	f.note(ctx)
	fn := f.symbols
	f.mu.Unlock()
	if fn == nil {
		return nil, status.Error(codes.Unimplemented, "no symbols")
	}
	return fn(req)
}

func (f *fakeSearchServer) Semantic(ctx gocontext.Context, req *ascv1.SemanticRequest) (*ascv1.SemanticResponse, error) {
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

func (f *fakeSearchServer) IndexStatus(ctx gocontext.Context, req *ascv1.IndexStatusRequest) (*ascv1.IndexStatusResponse, error) {
	f.mu.Lock()
	f.statuses = append(f.statuses, req)
	f.note(ctx)
	fn := f.index
	f.mu.Unlock()
	if fn == nil {
		return nil, status.Error(codes.Unimplemented, "no index status")
	}
	return fn(req)
}

func (f *fakeSearchServer) calls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.searches) + len(f.finds) + len(f.sems) + len(f.statuses)
}

func startFakeSearch(t *testing.T, f *fakeSearchServer) *fakeSearchServer {
	t.Helper()
	srv := grpc.NewServer()
	ascv1.RegisterCodeIntelServer(srv, f)
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	go func() { _ = srv.Serve(l) }()
	t.Cleanup(srv.Stop)
	useAsc(t, l.Addr().String())
	return f
}

func getAscSearch(t *testing.T, query string) ascGraphPage {
	t.Helper()
	ctx, resp := contexttest.MockContext(t, "/user2/repo1/asc/search"+query)
	ctx.Render = templates.HTMLRenderer()
	contexttest.LoadRepo(t, ctx, 1)
	ctx.Data["RepoLink"] = ctx.Repo.RepoLink
	AscSearch(ctx)
	return ascGraphPage{status: resp.Code, body: resp.Body.String(), ctx: ctx}
}

func indexStatusOf(entries ...*ascv1.IndexStatus) func(*ascv1.IndexStatusRequest) (*ascv1.IndexStatusResponse, error) {
	return func(*ascv1.IndexStatusRequest) (*ascv1.IndexStatusResponse, error) {
		return &ascv1.IndexStatusResponse{Indexes: entries}, nil
	}
}

func TestAscSearchLexical(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	setStorageBackend(t, 1, "asc")
	hit := &ascv1.FileHit{
		Path: []byte("src/a b/lib.rs"),
		Lang: "Rust",
		Matches: []*ascv1.LineMatch{{
			Line:   10,
			Cols:   []uint32{3},
			Text:   []byte("<b>needle</b> \x1b[31m ‮ x"),
			Before: [][]byte{[]byte("// before <i>")},
			After:  [][]byte{[]byte("// after")},
		}},
		AlsoAt: [][]byte{[]byte("vendor/copy.rs"), []byte("../bad")},
	}
	secret := &ascv1.FileHit{
		Path:     []byte("config/secrets.env"),
		Redacted: true,
		Matches:  []*ascv1.LineMatch{{Line: 1, Cols: []uint32{0}, Text: []byte("API_KEY=hunter2")}},
	}
	fake := startFakeSearch(t, &fakeSearchServer{
		search: func(*ascv1.SearchRequest) ([]*ascv1.SearchResponse, error) {
			return []*ascv1.SearchResponse{
				{Files: []*ascv1.FileHit{hit}, Scope: &ascv1.ResolvedScope{Coverage: 0.9}},
				{Files: []*ascv1.FileHit{secret}, Truncated: true},
			}, nil
		},
		index: indexStatusOf(
			&ascv1.IndexStatus{Index: "files", Coverage: 0.75, LagSeconds: 12},
			&ascv1.IndexStatus{Index: "chunks", Coverage: 0.75},
			&ascv1.IndexStatus{Index: "vectors", Coverage: 0.1},
		),
	})

	page := getAscSearch(t, "?q=needle&match=word&case=1&path=src&lang=Rust")
	require.Equal(t, http.StatusOK, page.status, page.body)

	require.Len(t, fake.searches, 1)
	req := fake.searches[0]
	assert.Equal(t, "needle", req.GetPattern())
	assert.Equal(t, ascv1.SearchMode_SEARCH_MODE_WORD, req.GetMode())
	assert.True(t, req.GetCaseSensitive())
	assert.Equal(t, "user2/repo1", req.GetScope().GetRepo().GetName())
	assert.Equal(t, "refs/heads/master", req.GetScope().GetRev())
	assert.Equal(t, []string{"src"}, req.GetScope().GetPaths())
	assert.Equal(t, []string{"Rust"}, req.GetScope().GetLangs())
	assert.Equal(t, []string{"Bearer " + ascRPCTestToken}, fake.auth[0], "the internal token, never a browser credential")

	// Links point into the revision and escape the path.
	assert.Contains(t, page.body, `href="/user2/repo1/src/branch/master/src/a%20b/lib.rs"`)
	assert.Contains(t, page.body, `href="/user2/repo1/src/branch/master/src/a%20b/lib.rs#L10"`)
	assert.Contains(t, page.body, `href="/user2/repo1/src/branch/master/vendor/copy.rs"`)
	assert.NotContains(t, page.body, "../bad")
	// The match is marked with the width of the search text, text is escaped, control and bidi characters go.
	assert.Contains(t, page.body, "&lt;b&gt;")
	assert.Contains(t, page.body, "&lt;i&gt;")
	assert.NotContains(t, page.body, "<b>needle")
	assert.NotContains(t, page.body, "\x1b")
	assert.NotContains(t, page.body, "‮")
	assert.Contains(t, page.body, "<mark>")
	// A redacted file shows its path but none of its text, even when the message carried some.
	assert.Contains(t, page.body, "config/secrets.env")
	assert.Contains(t, page.body, "asc_rp.s_hidden_file")
	assert.NotContains(t, page.body, "hunter2")
	assert.Contains(t, page.body, "asc_rp.s_more")
	// Coverage note: the files index of the mode, not the others.
	assert.Contains(t, page.body, "asc_rp.s_cov_part")
	assert.Contains(t, page.body, "files 75%")
	assert.NotContains(t, page.body, "vectors 10%")
	// The form shows the query again.
	assert.Contains(t, page.body, `value="needle"`)
	assert.Contains(t, page.body, `value="Rust"`)
}

func TestAscSearchLexicalModesAndScope(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	setStorageBackend(t, 1, "asc")
	fake := startFakeSearch(t, &fakeSearchServer{
		search: func(*ascv1.SearchRequest) ([]*ascv1.SearchResponse, error) { return nil, nil },
		index:  indexStatusOf(&ascv1.IndexStatus{Index: "files", Coverage: 1}),
	})

	sha := strings.Repeat("ab", 20)
	page := getAscSearch(t, "?q=a.%2Bb&match=regex&ref="+sha)
	require.Equal(t, http.StatusOK, page.status, page.body)
	req := fake.searches[len(fake.searches)-1]
	assert.Equal(t, ascv1.SearchMode_SEARCH_MODE_REGEX, req.GetMode())
	assert.False(t, req.GetCaseSensitive())
	assert.Equal(t, sha, req.GetScope().GetRev())
	assert.Empty(t, req.GetScope().GetPaths())
	assert.Empty(t, req.GetScope().GetLangs())
	assert.Contains(t, page.body, "asc_rp.s_nothing")
	assert.Contains(t, page.body, "asc_rp.s_cov_all")

	page = getAscSearch(t, "?q=x&match=nonsense")
	require.Equal(t, http.StatusOK, page.status, page.body)
	assert.Equal(t, ascv1.SearchMode_SEARCH_MODE_LITERAL, fake.searches[len(fake.searches)-1].GetMode())
}

func TestAscSearchSymbolMode(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	setStorageBackend(t, 1, "asc")
	evil := ascSym("<b>evil</b>", "src/lib.rs", 10, true, "name_match")
	evil.Doc = []byte("<script>alert(1)</script>")
	fake := startFakeSearch(t, &fakeSearchServer{
		symbols: func(*ascv1.SymbolsRequest) (*ascv1.SymbolsResponse, error) {
			return &ascv1.SymbolsResponse{Symbols: []*ascv1.Symbol{evil}, Scope: &ascv1.ResolvedScope{Coverage: 0.5}}, nil
		},
	})

	page := getAscSearch(t, "?mode=symbol&q=evil")
	require.Equal(t, http.StatusOK, page.status, page.body)
	require.Len(t, fake.finds, 1)
	assert.Equal(t, ascv1.SymbolOp_SYMBOL_OP_FIND, fake.finds[0].GetOp())
	assert.Equal(t, "evil", fake.finds[0].GetName())
	assert.Equal(t, "refs/heads/master", fake.finds[0].GetScope().GetRev())
	assert.Empty(t, fake.searches)
	assert.Contains(t, page.body, "/user2/repo1/src/branch/master/src/lib.rs#L10")
	assert.Contains(t, page.body, "name_match")
	assert.Contains(t, page.body, "&lt;b&gt;evil&lt;/b&gt;")
	assert.Contains(t, page.body, "&lt;script&gt;alert(1)&lt;/script&gt;")
	assert.NotContains(t, page.body, "<script>alert")
	// IndexStatus is unimplemented in this fake: the coverage of the answer's scope stands in for it.
	assert.Contains(t, page.body, "files 50%")
	assert.Contains(t, page.body, "asc_rp.s_cov_part")
}

func TestAscSearchSemanticMode(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	setStorageBackend(t, 1, "asc")
	fake := startFakeSearch(t, &fakeSearchServer{
		semantic: func(*ascv1.SemanticRequest) (*ascv1.SemanticResponse, error) {
			return &ascv1.SemanticResponse{
				Model: "embed-<v1>",
				Chunks: []*ascv1.Chunk{
					{Path: []byte("src/auth.rs"), StartLine: 4, EndLine: 9, Symbol: "check<T>", Score: 0.8123456,
						Sources: []string{"vector", "lexical"}, Text: []byte("fn check() {\n\t<script>x</script>\x1b[0m\n}")},
					{Path: []byte("config/.env"), StartLine: 1, EndLine: 1, Score: 0.5, Redacted: true, Text: []byte("TOKEN=hunter2")},
				},
			}, nil
		},
		index: indexStatusOf(
			&ascv1.IndexStatus{Index: "files", Coverage: 0.2},
			&ascv1.IndexStatus{Index: "vectors", Coverage: 1},
		),
	})

	page := getAscSearch(t, "?mode=semantic&q=where+is+auth+checked")
	require.Equal(t, http.StatusOK, page.status, page.body)
	require.Len(t, fake.sems, 1)
	assert.Equal(t, "where is auth checked", fake.sems[0].GetQuery())
	assert.Equal(t, ascv1.SemanticMode_SEMANTIC_MODE_HYBRID, fake.sems[0].GetMode())
	assert.EqualValues(t, ascSearchMaxChunks, fake.sems[0].GetK())
	assert.Equal(t, "refs/heads/master", fake.sems[0].GetScope().GetRev())
	assert.Contains(t, page.body, `href="/user2/repo1/src/branch/master/src/auth.rs#L4-L9"`)
	assert.Contains(t, page.body, "0.812")
	assert.Contains(t, page.body, "&lt;script&gt;x&lt;/script&gt;")
	assert.Contains(t, page.body, "check&lt;T&gt;")
	assert.Contains(t, page.body, "embed-&lt;v1&gt;")
	assert.NotContains(t, page.body, "<script>x")
	assert.NotContains(t, page.body, "\x1b")
	assert.NotContains(t, page.body, "hunter2")
	assert.Contains(t, page.body, "asc_rp.s_hidden_chunk")
	// The semantic mode reports the vectors index.
	assert.Contains(t, page.body, "vectors 100%")
	assert.NotContains(t, page.body, "files 20%")
	assert.Contains(t, page.body, "asc_rp.s_cov_all")
}

func TestAscSearchCoverageUnknown(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	setStorageBackend(t, 1, "asc")
	startFakeSearch(t, &fakeSearchServer{
		symbols: func(*ascv1.SymbolsRequest) (*ascv1.SymbolsResponse, error) { return &ascv1.SymbolsResponse{}, nil },
	})
	// No IndexStatus and no scope in the answer.
	page := getAscSearch(t, "?mode=symbol&q=x")
	require.Equal(t, http.StatusOK, page.status, page.body)
	assert.Contains(t, page.body, "asc_rp.s_cov_unknown")
}

func TestAscSearchEmptyFormMakesNoCall(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	setStorageBackend(t, 1, "asc")
	fake := startFakeSearch(t, &fakeSearchServer{})
	for _, q := range []string{"", "?mode=semantic", "?q=+++"} {
		page := getAscSearch(t, q)
		require.Equal(t, http.StatusOK, page.status, page.body)
		assert.Contains(t, page.body, `id="asc-search-form"`)
	}
	assert.Zero(t, fake.calls())
}

// TestAscSearchRefusesBadQueries: the call carries the internal token, so the scope may only be a revision
// Forgejo resolved. A change id, a stack, a workspace or a made-up branch never reaches asc.
func TestAscSearchRefusesBadQueries(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	setStorageBackend(t, 1, "asc")
	fake := startFakeSearch(t, &fakeSearchServer{})
	for name, c := range map[string]struct {
		query string
		code  int
	}{
		"change id as ref":   {"?q=x&ref=c:0123456789abcdef", http.StatusNotFound},
		"stack as ref":       {"?q=x&ref=stack:abc", http.StatusNotFound},
		"unknown branch":     {"?q=x&ref=nosuch", http.StatusNotFound},
		"option as ref":      {"?q=x&ref=--help", http.StatusBadRequest},
		"control in ref":     {"?q=x&ref=master%00x", http.StatusBadRequest},
		"upper case sha":     {"?q=x&ref=" + strings.Repeat("AB", 20), http.StatusBadRequest},
		"long ref":           {"?q=x&ref=" + strings.Repeat("a", ascSymbolsMaxRefBytes+1), http.StatusBadRequest},
		"path escapes":       {"?q=x&path=../etc", http.StatusBadRequest},
		"path with dot":      {"?q=x&path=a/./b", http.StatusBadRequest},
		"path with NUL":      {"?q=x&path=a%00b", http.StatusBadRequest},
		"path with slashes":  {"?q=x&path=a//b", http.StatusBadRequest},
		"language with glob": {"?q=x&lang=" + url.QueryEscape("Go*"), http.StatusBadRequest},
		"long language":      {"?q=x&lang=" + strings.Repeat("a", ascSearchMaxLangBytes+1), http.StatusBadRequest},
		"control in query":   {"?q=a%00b", http.StatusBadRequest},
		"bidi in query":      {"?q=a%E2%80%AEb", http.StatusBadRequest},
		"long query":         {"?q=" + strings.Repeat("a", ascSearchMaxQueryBytes+1), http.StatusBadRequest},
		"long symbol":        {"?mode=symbol&q=" + strings.Repeat("a", ascSearchMaxSymbolBytes+1), http.StatusBadRequest},
		"long question":      {"?mode=semantic&q=" + strings.Repeat("a", ascSearchMaxSemanticBytes+1), http.StatusBadRequest},
	} {
		t.Run(name, func(t *testing.T) {
			page := getAscSearch(t, c.query)
			assert.Equal(t, c.code, page.status)
			assert.Contains(t, page.body, `id="asc-search-form"`, "the form is still shown")
		})
	}
	assert.Zero(t, fake.calls(), "nothing reached asc")

	t.Run("not served for a repository outside asc or with asc off", func(t *testing.T) {
		defer func(v bool) { setting.Asc.Enabled = v }(setting.Asc.Enabled)
		setting.Asc.Enabled = false
		assert.Equal(t, http.StatusNotFound, getAscSearch(t, "?q=x").status)
		setting.Asc.Enabled = true
		setStorageBackend(t, 1, "git")
		assert.Equal(t, http.StatusNotFound, getAscSearch(t, "?q=x").status)
		setStorageBackend(t, 1, "asc")
		assert.Zero(t, fake.calls())
	})
}

// TestAscSearchEchoIsEscaped: what the form shows again is the browser's own input, and is escaped.
func TestAscSearchEchoIsEscaped(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	setStorageBackend(t, 1, "asc")
	startFakeSearch(t, &fakeSearchServer{})
	page := getAscSearch(t, "?q="+url.QueryEscape(`"><script>alert(1)</script>`)+"&path="+url.QueryEscape(`../"><img src=x onerror=alert(2)>`))
	assert.Equal(t, http.StatusBadRequest, page.status)
	assert.NotContains(t, page.body, "<script>alert(1)")
	assert.NotContains(t, page.body, "<img src=x")
}

func TestAscSearchRefusesBadAnswers(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	setStorageBackend(t, 1, "asc")
	var file *ascv1.FileHit
	var chunk *ascv1.Chunk
	startFakeSearch(t, &fakeSearchServer{
		search: func(*ascv1.SearchRequest) ([]*ascv1.SearchResponse, error) {
			return []*ascv1.SearchResponse{{Files: []*ascv1.FileHit{file}}}, nil
		},
		semantic: func(*ascv1.SemanticRequest) (*ascv1.SemanticResponse, error) {
			return &ascv1.SemanticResponse{Chunks: []*ascv1.Chunk{chunk}}, nil
		},
	})
	line := []*ascv1.LineMatch{{Line: 1, Text: []byte("x")}}
	for name, f := range map[string]*ascv1.FileHit{
		"parent path":    {Path: []byte("../secret"), Matches: line},
		"empty path":     {Path: nil, Matches: line},
		"double slash":   {Path: []byte("a//b"), Matches: line},
		"line zero":      {Path: []byte("a.rs"), Matches: []*ascv1.LineMatch{{Line: 0}}},
		"huge line":      {Path: []byte("a.rs"), Matches: []*ascv1.LineMatch{{Line: 1 << 30}}},
		"backslash path": {Path: []byte(`a\b`), Matches: line},
	} {
		t.Run("file "+name, func(t *testing.T) {
			file = f
			page := getAscSearch(t, "?q=x")
			assert.Equal(t, http.StatusBadGateway, page.status)
			assert.NotContains(t, page.body, "secret")
		})
	}
	for name, c := range map[string]*ascv1.Chunk{
		"parent path":    {Path: []byte("../secret"), StartLine: 1, EndLine: 2},
		"lines reversed": {Path: []byte("a.rs"), StartLine: 9, EndLine: 3},
		"huge line":      {Path: []byte("a.rs"), StartLine: 1, EndLine: 1 << 30},
	} {
		t.Run("chunk "+name, func(t *testing.T) {
			chunk = c
			assert.Equal(t, http.StatusBadGateway, getAscSearch(t, "?mode=semantic&q=x").status)
		})
	}
	t.Run("chunk with a bad score", func(t *testing.T) {
		chunk = &ascv1.Chunk{Path: []byte("a.rs"), StartLine: 1, EndLine: 1, Score: math.NaN()}
		assert.Equal(t, http.StatusBadGateway, getAscSearch(t, "?mode=semantic&q=x").status)
	})
}

func TestAscSearchErrorsAreGeneric(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	setStorageBackend(t, 1, "asc")
	secret := "sqlite error at /var/lib/asc/asc.db password=hunter2"
	var answer error
	startFakeSearch(t, &fakeSearchServer{
		search:  func(*ascv1.SearchRequest) ([]*ascv1.SearchResponse, error) { return nil, answer },
		symbols: func(*ascv1.SymbolsRequest) (*ascv1.SymbolsResponse, error) { return nil, answer },
	})
	for name, c := range map[string]struct {
		err  error
		code int
	}{
		"not found":     {status.Error(codes.NotFound, secret), http.StatusNotFound},
		"invalid":       {status.Error(codes.InvalidArgument, secret), http.StatusBadRequest},
		"exhausted":     {status.Error(codes.ResourceExhausted, secret), http.StatusTooManyRequests},
		"unavailable":   {status.Error(codes.Unavailable, secret), http.StatusServiceUnavailable},
		"not indexed":   {status.Error(codes.FailedPrecondition, secret), http.StatusServiceUnavailable},
		"deadline":      {status.Error(codes.DeadlineExceeded, secret), http.StatusGatewayTimeout},
		"internal":      {status.Error(codes.Internal, secret), http.StatusBadGateway},
		"unauthorised":  {status.Error(codes.PermissionDenied, secret), http.StatusBadGateway},
		"not a status":  {assert.AnError, http.StatusBadGateway},
		"unauthorised2": {status.Error(codes.Unauthenticated, secret), http.StatusBadGateway},
	} {
		t.Run(name, func(t *testing.T) {
			answer = c.err
			for _, q := range []string{"?q=x", "?mode=symbol&q=x"} {
				page := getAscSearch(t, q)
				assert.Equal(t, c.code, page.status, q)
				assert.NotContains(t, page.body, "hunter2")
				assert.NotContains(t, page.body, "sqlite")
			}
		})
	}
}

func TestAscSearchNoConnection(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	setStorageBackend(t, 1, "asc")
	useStub(t, status.Error(codes.Unavailable, "connection refused to 10.0.0.1"))
	page := getAscSearch(t, "?q=x")
	assert.Equal(t, http.StatusServiceUnavailable, page.status)
	assert.NotContains(t, page.body, "10.0.0.1")
}

func TestAscSearchSegments(t *testing.T) {
	q := ascSearchQuery{mode: ascSearchModeLexical, match: ascSearchMatchLiteral, text: "foo"}
	segs := func(raw string, cols []uint32, q ascSearchQuery) []ascSearchSeg {
		return ascSearchSegments([]byte(raw), cols, q)
	}

	assert.Equal(t, []ascSearchSeg{{Text: "x "}, {Text: "foo", Hit: true}, {Text: " y"}}, segs("x foo y", []uint32{2}, q))
	assert.Equal(t, []ascSearchSeg{{Text: "x "}, {Text: "FOO", Hit: true}}, segs("x FOO", []uint32{2}, q), "case-insensitive literal")
	assert.Equal(t, []ascSearchSeg{{Text: "foo", Hit: true}, {Text: " "}, {Text: "foo", Hit: true}}, segs("foo foo", []uint32{0, 4}, q))
	// Where the text there is not the search text, and for a regex, a match is one character.
	assert.Equal(t, []ascSearchSeg{{Text: "a"}, {Text: "b", Hit: true}, {Text: "c"}}, segs("abc", []uint32{1}, q))
	rx := ascSearchQuery{mode: ascSearchModeLexical, match: ascSearchMatchRegex, text: "fo+"}
	assert.Equal(t, []ascSearchSeg{{Text: "f", Hit: true}, {Text: "oo"}}, segs("foo", []uint32{0}, rx))
	// A column past the end, in the middle of a character, or going back is ignored.
	assert.Equal(t, []ascSearchSeg{{Text: "é"}}, segs("é", []uint32{1, 9}, q))
	assert.Equal(t, []ascSearchSeg{{Text: "foo", Hit: true}}, segs("foo", []uint32{0, 0}, q))
	// Cleaning is per piece, so a column never moves.
	assert.Equal(t, []ascSearchSeg{{Text: "  "}, {Text: "foo", Hit: true}}, segs("\x00\x1bfoo", []uint32{2}, q))
	assert.Equal(t, []ascSearchSeg{{Text: ""}}, segs("", nil, q))
	assert.Equal(t, []ascSearchSeg{{Text: "plain"}}, segs("pl‮ain", nil, q))
}

func TestAscSearchTexts(t *testing.T) {
	assert.Equal(t, "a b c", ascSearchClean([]byte("a\x00b\tc")))
	assert.Equal(t, "ab", ascSearchClean([]byte("a‮b")))
	assert.Equal(t, "a�b", ascSearchClean([]byte("a\xffb")))
	assert.Equal(t, "l1\nl2", ascSearchBlock([]byte("l1\r\nl2\n\n")))
	assert.Equal(t, "a b", ascSearchBlock([]byte("a\x1bb")))
	long := strings.Repeat("x\n", ascSearchMaxChunkLines*2)
	assert.Len(t, strings.Split(ascSearchBlock([]byte(long)), "\n"), ascSearchMaxChunkLines)
	assert.True(t, ascSearchLangOK("C++"))
	assert.True(t, ascSearchLangOK("Objective-C"))
	assert.True(t, ascSearchLangOK("F#"))
	assert.False(t, ascSearchLangOK(""))
	assert.False(t, ascSearchLangOK("Go\n"))
	assert.False(t, ascSearchLangOK("a/b"))
	assert.False(t, ascSearchLangOK("a\"b"))
}
