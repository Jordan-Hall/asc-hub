// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package explore

import (
	"context"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"

	"forgejo.org/models/db"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/ascclient"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/templates"
	"forgejo.org/modules/test"
	"forgejo.org/services/ascsearch"
	"forgejo.org/services/contexttest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

const exInternal = "internal-token-of-the-hub"

// exFake is an asc serve. A nil handler answers Unimplemented.
type exFake struct {
	mu   sync.Mutex
	auth []string

	cross []*ascv1.CrossSearchRequest
	look  []*ascv1.LookupSymbolRequest
	srch  []*ascv1.SearchRequest
	sem   []*ascv1.SemanticRequest
	stat  []*ascv1.IndexStatusRequest

	onCross  func(*ascv1.CrossSearchRequest) ([]*ascv1.CrossSearchResponse, error)
	onLookup func(*ascv1.LookupSymbolRequest) (*ascv1.LookupSymbolResponse, error)
	onSearch func(*ascv1.SearchRequest) ([]*ascv1.SearchResponse, error)
	onSem    func(*ascv1.SemanticRequest) (*ascv1.SemanticResponse, error)
	onStatus func(*ascv1.IndexStatusRequest) (*ascv1.IndexStatusResponse, error)
}

func (f *exFake) note(ctx context.Context) {
	md, _ := metadata.FromIncomingContext(ctx)
	f.auth = append(f.auth, strings.Join(md.Get("authorization"), ","))
}

func (f *exFake) calls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.cross) + len(f.look) + len(f.srch) + len(f.sem) + len(f.stat)
}

type exCross struct {
	ascv1.UnimplementedCrossSearchServer
	f *exFake
}

func (c exCross) Search(req *ascv1.CrossSearchRequest, s grpc.ServerStreamingServer[ascv1.CrossSearchResponse]) error {
	f := c.f
	f.mu.Lock()
	f.cross = append(f.cross, req)
	f.note(s.Context())
	fn := f.onCross
	f.mu.Unlock()
	if fn == nil {
		return status.Error(codes.Unimplemented, "x")
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

func (c exCross) LookupSymbol(ctx context.Context, req *ascv1.LookupSymbolRequest) (*ascv1.LookupSymbolResponse, error) {
	f := c.f
	f.mu.Lock()
	f.look = append(f.look, req)
	f.note(ctx)
	fn := f.onLookup
	f.mu.Unlock()
	if fn == nil {
		return nil, status.Error(codes.Unimplemented, "x")
	}
	return fn(req)
}

type exIntel struct {
	ascv1.UnimplementedCodeIntelServer
	f *exFake
}

func (c exIntel) Search(req *ascv1.SearchRequest, s grpc.ServerStreamingServer[ascv1.SearchResponse]) error {
	f := c.f
	f.mu.Lock()
	f.srch = append(f.srch, req)
	f.note(s.Context())
	fn := f.onSearch
	f.mu.Unlock()
	if fn == nil {
		return status.Error(codes.Unimplemented, "x")
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

func (c exIntel) Symbols(ctx context.Context, req *ascv1.SymbolsRequest) (*ascv1.SymbolsResponse, error) {
	return nil, status.Error(codes.Unimplemented, "x")
}

func (c exIntel) Semantic(ctx context.Context, req *ascv1.SemanticRequest) (*ascv1.SemanticResponse, error) {
	f := c.f
	f.mu.Lock()
	f.sem = append(f.sem, req)
	f.note(ctx)
	fn := f.onSem
	f.mu.Unlock()
	if fn == nil {
		return nil, status.Error(codes.Unimplemented, "x")
	}
	return fn(req)
}

func (c exIntel) IndexStatus(ctx context.Context, req *ascv1.IndexStatusRequest) (*ascv1.IndexStatusResponse, error) {
	f := c.f
	f.mu.Lock()
	f.stat = append(f.stat, req)
	f.note(ctx)
	fn := f.onStatus
	f.mu.Unlock()
	if fn == nil {
		return nil, status.Error(codes.Unimplemented, "x")
	}
	return fn(req)
}

func useFake(t *testing.T, f *exFake) {
	t.Helper()
	srv := grpc.NewServer()
	ascv1.RegisterCrossSearchServer(srv, exCross{f: f})
	ascv1.RegisterCodeIntelServer(srv, exIntel{f: f})
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	go func() { _ = srv.Serve(l) }()
	t.Cleanup(srv.Stop)
	c, err := ascclient.New(ascclient.Config{Target: l.Addr().String(), InternalToken: exInternal, Retry: ascclient.RetryConfig{MaxAttempts: 1}})
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Close() })
	t.Cleanup(ascsearch.UseConn(c))
	t.Cleanup(test.MockVariableValue(&setting.Asc.Enabled, true))
}

func setBackend(t *testing.T, ids ...int64) {
	t.Helper()
	for _, id := range ids {
		_, err := db.GetEngine(db.DefaultContext).ID(id).Cols("storage_backend").Update(&repo_model.Repository{StorageBackend: "asc"})
		require.NoError(t, err)
	}
}

type exPage struct {
	status int
	body   string
}

// get renders the page as user id (0 is anonymous).
func get(t *testing.T, userID int64, query string) exPage {
	t.Helper()
	ctx, resp := contexttest.MockContext(t, "/explore/code"+query)
	ctx.Render = templates.HTMLRenderer()
	if userID != 0 {
		ctx.Doer = unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: userID})
	}
	CodeSearch(ctx)
	return exPage{status: resp.Code, body: resp.Body.String()}
}

func ref(n string) *ascv1.RepoRef { return &ascv1.RepoRef{Name: n} }

func hit(path, text string, cols ...uint32) *ascv1.FileHit {
	return &ascv1.FileHit{Path: []byte(path), Lang: "Rust", Matches: []*ascv1.LineMatch{{
		Line: 10, Cols: cols, Text: []byte(text), Before: [][]byte{[]byte("// before <i>")}, After: [][]byte{[]byte("// after")},
	}}}
}

func TestAscExploreText(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	setBackend(t, 1, 2, 4)
	const key = "AKIAIOSFODNN7EXAMPLE"
	f := &exFake{
		onCross: func(req *ascv1.CrossSearchRequest) ([]*ascv1.CrossSearchResponse, error) {
			var hits []*ascv1.RepoHits
			for _, n := range req.GetTarget().GetRepos() {
				files := []*ascv1.FileHit{
					hit("src/a b/lib.rs", "<b>retry</b> \x1b[31m ‮ retry", 3, 24),
					{Path: []byte("config/secrets.env"), Redacted: true, Matches: []*ascv1.LineMatch{{Line: 1, Text: []byte("API_KEY=" + key)}}},
				}
				hits = append(hits, &ascv1.RepoHits{Repo: ref(n), Files: files, Scope: &ascv1.ResolvedScope{Coverage: 1}})
			}
			hits = append(hits, &ascv1.RepoHits{Repo: ref("secret/hidden"), Files: []*ascv1.FileHit{hit("leak.rs", "retry leaked", 0)}})
			return []*ascv1.CrossSearchResponse{{Repos: hits, SearchedRepos: 3, EstTokens: 1}}, nil
		},
		onLookup: func(req *ascv1.LookupSymbolRequest) (*ascv1.LookupSymbolResponse, error) {
			return &ascv1.LookupSymbolResponse{Hits: []*ascv1.SymbolHit{
				{Repo: ref("user2/repo1"), Symbol: &ascv1.Symbol{Name: "retry", Kind: "fn", IsDef: true, Path: []byte("src/a b/lib.rs"), Line: 10, EndLine: 12, Confidence: "precise"}},
			}}, nil
		},
		onStatus: func(*ascv1.IndexStatusRequest) (*ascv1.IndexStatusResponse, error) {
			return &ascv1.IndexStatusResponse{Indexes: []*ascv1.IndexStatus{{Index: "files", Coverage: 0.5, LagSeconds: 9}}}, nil
		},
	}
	useFake(t, f)

	page := get(t, 2, "?q=retry&match=word&case=1&lang=Rust&path=src")
	require.Equal(t, http.StatusOK, page.status, page.body)

	require.Len(t, f.cross, 1)
	req := f.cross[0]
	assert.ElementsMatch(t, []string{"user2/repo1", "user2/repo2", "user5/repo4"}, req.GetTarget().GetRepos(), "what user2 can read")
	assert.Equal(t, "retry", req.GetPattern())
	assert.Equal(t, ascv1.SearchMode_SEARCH_MODE_WORD, req.GetMode())
	assert.True(t, req.GetCaseSensitive())
	assert.Equal(t, []string{"Rust"}, req.GetLangs())
	assert.Equal(t, []string{"src"}, req.GetPaths())
	for _, a := range f.auth {
		assert.Equal(t, "Bearer "+exInternal, a, "a browser session holds no asc token: the internal token, with Forgejo naming the repositories")
	}

	// Links point into the default branch and escape the path.
	assert.Contains(t, page.body, `href="/user2/repo1/src/branch/master/src/a%20b/lib.rs"`)
	assert.Contains(t, page.body, `href="/user2/repo1/src/branch/master/src/a%20b/lib.rs#L10"`)
	assert.Contains(t, page.body, `href="/user5/repo4"`)
	// The text is escaped, control and bidi characters are gone, and the matches are marked.
	assert.Contains(t, page.body, "&lt;b&gt;")
	assert.NotContains(t, page.body, "<b>retry")
	assert.NotContains(t, page.body, "\x1b")
	assert.NotContains(t, page.body, "‮")
	assert.Contains(t, page.body, "<mark>retry</mark>")
	// A redacted file shows its path but none of its text, and a repository nobody asked for is not shown.
	assert.Contains(t, page.body, "config/secrets.env")
	assert.Contains(t, page.body, "hidden because it may hold secrets")
	assert.NotContains(t, page.body, key)
	assert.NotContains(t, page.body, "secret/hidden")
	assert.NotContains(t, page.body, "leaked")
	// The file that declares the word is marked, and the word is listed.
	assert.Contains(t, page.body, "Definition")
	assert.Contains(t, page.body, "Symbols matched: retry")
	assert.Contains(t, page.body, `/user2/repo1/asc/symbols?op=def&amp;name=retry`)
	// Facets and coverage.
	assert.Contains(t, page.body, "Language")
	assert.Contains(t, page.body, "Rust")
	assert.Contains(t, page.body, "mode=text")
	assert.Contains(t, page.body, "50% of files")
	assert.Contains(t, page.body, "Results may be incomplete")
	assert.Contains(t, page.body, "Try meaning search")
	// The form shows the query again.
	assert.Contains(t, page.body, `value="retry"`)
}

func TestAscExploreVisitorSeesPublicRepositoriesOnly(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	setBackend(t, 1, 2, 3, 4)
	f := &exFake{onCross: func(req *ascv1.CrossSearchRequest) ([]*ascv1.CrossSearchResponse, error) {
		return nil, nil
	}}
	useFake(t, f)

	page := get(t, 0, "?q=anything")
	require.Equal(t, http.StatusOK, page.status, page.body)
	require.Len(t, f.cross, 1)
	assert.ElementsMatch(t, []string{"user2/repo1", "user5/repo4"}, f.cross[0].GetTarget().GetRepos(), "no private repository for a visitor")
	assert.NotContains(t, page.body, "repo2")
	assert.NotContains(t, page.body, "repo3")
	assert.Contains(t, page.body, "Nothing found")

	f.cross = nil
	page = get(t, 5, "?q=anything&owner=user2")
	require.Equal(t, http.StatusOK, page.status, page.body)
	assert.Equal(t, []string{"user2/repo1"}, f.cross[0].GetTarget().GetRepos(), "user5 cannot read user2/repo2")
}

func TestAscExploreSymbolAndMeaning(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	setBackend(t, 1, 4)
	f := &exFake{
		onLookup: func(req *ascv1.LookupSymbolRequest) (*ascv1.LookupSymbolResponse, error) {
			return &ascv1.LookupSymbolResponse{Hits: []*ascv1.SymbolHit{
				{Repo: ref("user2/repo1"), Symbol: &ascv1.Symbol{Name: "RetryPolicy", Kind: "struct", IsDef: true, Path: []byte("src/retry.rs"), Line: 4, EndLine: 9, Signature: []byte("pub struct RetryPolicy<T>"), Container: "payments", Doc: []byte("<script>x</script>"), Confidence: "precise"}, Score: 1},
				{Repo: ref("user5/repo4"), Symbol: &ascv1.Symbol{Name: "retry", Kind: "fn", Path: []byte("a.rs"), Line: 1, EndLine: 1, Confidence: "name_match"}, Score: 0.5},
				{Repo: ref("hidden/repo"), Symbol: &ascv1.Symbol{Name: "retry", Kind: "fn", Path: []byte("z.rs"), Line: 1, EndLine: 1, Confidence: "precise"}, Score: 1},
			}}, nil
		},
		onSem: func(req *ascv1.SemanticRequest) (*ascv1.SemanticResponse, error) {
			if req.GetScope().GetRepo().GetName() == "user5/repo4" {
				return nil, status.Error(codes.PermissionDenied, "no")
			}
			return &ascv1.SemanticResponse{Model: "embed-1", Chunks: []*ascv1.Chunk{
				{Path: []byte("src/refund.rs"), StartLine: 18, EndLine: 24, Symbol: "stop_retry", Score: 0.91, Sources: []string{"vector"}, Text: []byte("fn stop_retry() {\n    <b>x</b>\n}")},
				{Path: []byte("s.env"), Redacted: true, Text: []byte("AKIAIOSFODNN7EXAMPLE")},
			}}, nil
		},
	}
	useFake(t, f)

	page := get(t, 2, "?mode=symbol&q=retry")
	require.Equal(t, http.StatusOK, page.status, page.body)
	assert.Equal(t, ascv1.SymbolMatch_SYMBOL_MATCH_FUZZY, f.look[0].GetMatch())
	assert.Contains(t, page.body, "RetryPolicy")
	assert.Contains(t, page.body, "pub struct RetryPolicy&lt;T&gt;")
	assert.NotContains(t, page.body, "<script>x")
	assert.Contains(t, page.body, "&lt;script&gt;x")
	assert.Contains(t, page.body, "precise")
	assert.Contains(t, page.body, "name_match")
	assert.Contains(t, page.body, `href="/user2/repo1/src/branch/master/src/retry.rs#L4"`)
	assert.Contains(t, page.body, `/user2/repo1/asc/symbols?op=def&amp;name=RetryPolicy`)
	assert.NotContains(t, page.body, "hidden/repo")
	assert.Contains(t, page.body, "2 symbols")

	page = get(t, 2, "?mode=meaning&q=where+do+we+stop+retrying+a+refund")
	require.Equal(t, http.StatusOK, page.status, page.body)
	require.Len(t, f.sem, 2, "one query embedding per repository")
	assert.Contains(t, page.body, "embed-1")
	assert.Contains(t, page.body, "stop_retry")
	assert.Contains(t, page.body, "0.910")
	assert.Contains(t, page.body, "&lt;b&gt;x&lt;/b&gt;")
	assert.Contains(t, page.body, "hidden because it may hold secrets")
	assert.NotContains(t, page.body, "AKIA")
	assert.Contains(t, page.body, `href="/user2/repo1/src/branch/master/src/refund.rs#L18-L24"`)
}

func TestAscExploreFallbackWithoutCrossSearch(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	setBackend(t, 1, 4)
	f := &exFake{onSearch: func(req *ascv1.SearchRequest) ([]*ascv1.SearchResponse, error) {
		if req.GetScope().GetRepo().GetName() == "user5/repo4" {
			return nil, status.Error(codes.NotFound, "x")
		}
		return []*ascv1.SearchResponse{{Files: []*ascv1.FileHit{hit("a.rs", "needle here", 0)}}}, nil
	}}
	useFake(t, f) // CrossSearch answers Unimplemented

	page := get(t, 2, "?q=needle")
	require.Equal(t, http.StatusOK, page.status, page.body)
	assert.Contains(t, page.body, "<mark>needle</mark>")
	assert.Contains(t, page.body, `href="/user2/repo1"`)
	assert.NotContains(t, page.body, `href="/user5/repo4"`, "a repository asc refuses adds nothing")
}

func TestAscExploreChecks(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	setBackend(t, 1)
	f := &exFake{onCross: func(*ascv1.CrossSearchRequest) ([]*ascv1.CrossSearchResponse, error) { return nil, nil }}
	useFake(t, f)

	for name, c := range map[string]struct{ query, msg string }{
		"short":      {"?q=ab", "Type at least 3 characters"},
		"control":    {"?q=ab%0Acd", "cannot be searched for"},
		"long":       {"?q=" + strings.Repeat("a", 4097), "too long"},
		"owner":      {"?q=abc&owner=a/b", "owner is not a name"},
		"language 2": {"?q=abc&lang=a%3Bb", "not a language name"},
		"path":       {"?q=abc&path=../x", "not a path"},
		"token":      {"?q=abc&page_token=a%20b", "page token is not valid"},
		"long name":  {"?mode=symbol&q=" + strings.Repeat("a", 257), "cannot be searched for"},
		"long ask":   {"?mode=meaning&q=" + strings.Repeat("a", 1001), "cannot be searched for"},
	} {
		page := get(t, 2, c.query)
		assert.Equal(t, http.StatusBadRequest, page.status, name)
		assert.Contains(t, page.body, c.msg, name)
	}
	assert.Zero(t, f.calls(), "nothing bad reaches asc")

	// An empty query is the empty form.
	page := get(t, 2, "")
	assert.Equal(t, http.StatusOK, page.status)
	assert.Contains(t, page.body, "Search the code of every repository you can read")
	page = get(t, 2, "?q=")
	assert.Equal(t, http.StatusOK, page.status)
	assert.Zero(t, f.calls())

	// Unknown modes and matches fall back to the defaults.
	page = get(t, 2, "?q=abc&mode=nonsense&match=nonsense")
	assert.Equal(t, http.StatusOK, page.status, page.body)
	assert.Equal(t, ascv1.SearchMode_SEARCH_MODE_LITERAL, f.cross[0].GetMode())
}

func TestAscExploreNoRepositories(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	f := &exFake{}
	useFake(t, f) // no repository lives in asc
	page := get(t, 2, "?q=abc")
	assert.Equal(t, http.StatusOK, page.status)
	assert.Contains(t, page.body, "No repository you can read is stored in asc")
	assert.Zero(t, f.calls(), "no repository, no call")
}

func TestAscExploreErrors(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	setBackend(t, 1)
	f := &exFake{}
	useFake(t, f)
	tooBroad := func() error {
		st, _ := status.New(codes.InvalidArgument, "SECRET-DETAIL").WithDetails(&ascv1.ErrorDetail{Code: "SEARCH_TOO_BROAD"})
		return st.Err()
	}
	for name, c := range map[string]struct {
		err    error
		status int
		msg    string
	}{
		"internal": {status.Error(codes.Internal, "SECRET-DETAIL /srv/x"), http.StatusBadGateway, "not available right now"},
		"down":     {status.Error(codes.Unavailable, "SECRET-DETAIL"), http.StatusServiceUnavailable, "not available right now"},
		"limited":  {status.Error(codes.ResourceExhausted, "SECRET-DETAIL"), http.StatusTooManyRequests, "Too many searches"},
		"slow":     {status.Error(codes.DeadlineExceeded, "SECRET-DETAIL"), http.StatusGatewayTimeout, "took too long"},
		"broad":    {tooBroad(), http.StatusBadRequest, "matches too much"},
		"refused":  {status.Error(codes.InvalidArgument, "SECRET-DETAIL"), http.StatusBadRequest, "not valid"},
	} {
		f.onCross = func(*ascv1.CrossSearchRequest) ([]*ascv1.CrossSearchResponse, error) { return nil, c.err }
		page := get(t, 2, "?q=abc")
		assert.Equal(t, c.status, page.status, name)
		assert.Contains(t, page.body, c.msg, name)
		assert.NotContains(t, page.body, "SECRET-DETAIL", name)
		assert.NotContains(t, page.body, "/srv/x", name)
	}

	t.Run("no connection", func(t *testing.T) {
		restore := ascsearch.UseConn(nil)
		defer restore()
		defer test.MockVariableValue(&setting.InternalToken, "")()
		page := get(t, 2, "?q=abc")
		assert.Equal(t, http.StatusBadGateway, page.status)
		assert.Contains(t, page.body, "not available right now")
	})

	t.Run("too many searches at once", func(t *testing.T) {
		for range ascExploreMaxConcurrent {
			ascExploreSlots <- struct{}{}
		}
		defer func() {
			for range ascExploreMaxConcurrent {
				<-ascExploreSlots
			}
		}()
		calls := f.calls()
		page := get(t, 2, "?q=abc")
		assert.Equal(t, http.StatusTooManyRequests, page.status)
		assert.Equal(t, calls, f.calls())
	})
}

func TestAscExplorePaging(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	setBackend(t, 1)
	f := &exFake{onCross: func(req *ascv1.CrossSearchRequest) ([]*ascv1.CrossSearchResponse, error) {
		return []*ascv1.CrossSearchResponse{{
			Repos:         []*ascv1.RepoHits{{Repo: ref("user2/repo1"), Files: []*ascv1.FileHit{hit("a.rs", "needle", 0)}}},
			Truncated:     true,
			NextPageToken: "tok-2",
		}}, nil
	}}
	useFake(t, f)
	page := get(t, 2, "?q=needle&lang=Rust")
	require.Equal(t, http.StatusOK, page.status, page.body)
	assert.Contains(t, page.body, "page_token=tok-2")
	assert.Contains(t, page.body, "lang=Rust")
	assert.Contains(t, page.body, "More results")

	page = get(t, 2, "?q=needle&page_token=tok-2")
	require.Equal(t, http.StatusOK, page.status, page.body)
	assert.Equal(t, "tok-2", f.cross[len(f.cross)-1].GetPageToken())
	// A continued search does not look the words up again.
	n := len(f.look)
	_ = get(t, 2, "?q=needle&page_token=tok-3")
	assert.Equal(t, n, len(f.look))
}

func TestAscExploreFallsBackToTheStockPage(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	defer test.MockVariableValue(&setting.Asc.Enabled, false)()
	defer test.MockVariableValue(&setting.Indexer.RepoIndexerEnabled, false)()
	ctx, resp := contexttest.MockContext(t, "/explore/code?q=abc")
	CodeSearch(ctx)
	assert.Equal(t, http.StatusSeeOther, resp.Code, "the stock page redirects while its indexer is off")
	assert.Equal(t, "/explore", resp.Header().Get("Location"))
}

func TestAscExploreHelpers(t *testing.T) {
	q := ascExploreQuery{mode: "text", text: "a b&c", match: "word", cs: true, owner: "o", lang: "Go", path: "src"}
	u, err := url.Parse(q.url(map[string]string{"lang": "", "page_token": "t"}))
	require.NoError(t, err)
	v := u.Query()
	assert.Equal(t, "a b&c", v.Get("q"))
	assert.Equal(t, "word", v.Get("match"))
	assert.Equal(t, "1", v.Get("case"))
	assert.Equal(t, "o", v.Get("owner"))
	assert.False(t, v.Has("lang"), "a key set to empty is dropped")
	assert.Equal(t, "t", v.Get("page_token"))
	assert.Equal(t, "/explore/code", u.Path)

	sym := ascExploreQuery{mode: "symbol", text: "x", match: "regex", cs: true}
	v2, _ := url.ParseQuery(strings.TrimPrefix(sym.url(nil), "/explore/code?"))
	assert.False(t, v2.Has("match"), "text options are only for text")
	assert.False(t, v2.Has("case"))

	r := ascsearch.Repo{Link: "/o/r", Branch: "feat/x y"}
	assert.Equal(t, "/o/r/src/branch/feat/x%20y/a%20b/c.rs", fileLink(r, "a b/c.rs", 0, 0))
	assert.Equal(t, "/o/r/src/branch/feat/x%20y/c.rs#L4", fileLink(r, "c.rs", 4, 0))
	assert.Equal(t, "/o/r/src/branch/feat/x%20y/c.rs#L4-L9", fileLink(r, "c.rs", 4, 9))

	lines := ascExploreLines(r, ascsearch.FileHit{Path: "c.rs", Matches: []ascsearch.LineMatch{
		{Line: 5, Cols: []uint32{0}, Text: "needle", Before: []string{"b4"}, After: []string{"a6", "a7"}},
		{Line: 7, Cols: []uint32{1}, Text: "xneedle", Before: []string{"b6"}, After: []string{"a8"}},
	}}, "needle")
	var nums []uint32
	for _, l := range lines {
		nums = append(nums, l.Number)
	}
	assert.Equal(t, []uint32{4, 5, 6, 7, 8}, nums, "a line an earlier match drew is not drawn twice")
	assert.True(t, lines[1].IsMatch)
	assert.True(t, lines[1].Segments[0].Hit)
	assert.True(t, lines[3].IsMatch, "a match replaces the same line drawn as context")
	assert.Equal(t, "needle", lines[3].Segments[1].Text)
	assert.True(t, lines[3].Segments[1].Hit)
	assert.Nil(t, ascExploreLines(r, ascsearch.FileHit{Path: "c.rs", Redacted: true, Matches: []ascsearch.LineMatch{{Line: 1}}}, ""))
}
