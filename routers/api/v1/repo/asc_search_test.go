// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	"context"
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"

	"forgejo.org/models/db"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unittest"
	"forgejo.org/modules/ascclient"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/json"
	"forgejo.org/modules/setting"
	api "forgejo.org/modules/structs"
	"forgejo.org/modules/test"
	"forgejo.org/services/ascsearch"
	"forgejo.org/services/authz"
	api_context "forgejo.org/services/context"
	"forgejo.org/services/contexttest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

const ascAPIInternal = "internal-token-never-forwarded"

// ascAPIFake is an asc serve for the cross-repository routes. A nil handler answers Unimplemented.
type ascAPIFake struct {
	mu   sync.Mutex
	auth []string

	cross   []*ascv1.CrossSearchRequest
	look    []*ascv1.LookupSymbolRequest
	panel   []*ascv1.SymbolPanelRequest
	srch    []*ascv1.SearchRequest
	sem     []*ascv1.SemanticRequest
	statusC []*ascv1.IndexStatusRequest
	symb    []*ascv1.SymbolsRequest

	onSymbols  func(*ascv1.SymbolsRequest) (*ascv1.SymbolsResponse, error)
	onCross    func(*ascv1.CrossSearchRequest) ([]*ascv1.CrossSearchResponse, error)
	onLookup   func(*ascv1.LookupSymbolRequest) (*ascv1.LookupSymbolResponse, error)
	onPanel    func(*ascv1.SymbolPanelRequest) (*ascv1.SymbolPanelResponse, error)
	onSemantic func(*ascv1.SemanticRequest) (*ascv1.SemanticResponse, error)
	onStatus   func(*ascv1.IndexStatusRequest) (*ascv1.IndexStatusResponse, error)
}

func (f *ascAPIFake) note(ctx context.Context) {
	md, _ := metadata.FromIncomingContext(ctx)
	f.auth = append(f.auth, strings.Join(md.Get("authorization"), ","))
}

type ascAPICross struct {
	ascv1.UnimplementedCrossSearchServer
	f *ascAPIFake
}

func (c ascAPICross) Search(req *ascv1.CrossSearchRequest, s grpc.ServerStreamingServer[ascv1.CrossSearchResponse]) error {
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

func (c ascAPICross) LookupSymbol(ctx context.Context, req *ascv1.LookupSymbolRequest) (*ascv1.LookupSymbolResponse, error) {
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

func (c ascAPICross) SymbolPanel(ctx context.Context, req *ascv1.SymbolPanelRequest) (*ascv1.SymbolPanelResponse, error) {
	f := c.f
	f.mu.Lock()
	f.panel = append(f.panel, req)
	f.note(ctx)
	fn := f.onPanel
	f.mu.Unlock()
	if fn == nil {
		return nil, status.Error(codes.Unimplemented, "x")
	}
	return fn(req)
}

type ascAPIIntel struct {
	ascv1.UnimplementedCodeIntelServer
	f *ascAPIFake
}

func (c ascAPIIntel) Search(req *ascv1.SearchRequest, s grpc.ServerStreamingServer[ascv1.SearchResponse]) error {
	c.f.mu.Lock()
	c.f.srch = append(c.f.srch, req)
	c.f.note(s.Context())
	c.f.mu.Unlock()
	return s.Send(&ascv1.SearchResponse{})
}

func (c ascAPIIntel) Symbols(ctx context.Context, req *ascv1.SymbolsRequest) (*ascv1.SymbolsResponse, error) {
	f := c.f
	f.mu.Lock()
	f.symb = append(f.symb, req)
	f.note(ctx)
	fn := f.onSymbols
	f.mu.Unlock()
	if fn == nil {
		return nil, status.Error(codes.Unimplemented, "x")
	}
	return fn(req)
}

func (c ascAPIIntel) Semantic(ctx context.Context, req *ascv1.SemanticRequest) (*ascv1.SemanticResponse, error) {
	f := c.f
	f.mu.Lock()
	f.sem = append(f.sem, req)
	f.note(ctx)
	fn := f.onSemantic
	f.mu.Unlock()
	if fn == nil {
		return nil, status.Error(codes.Unimplemented, "x")
	}
	return fn(req)
}

func (c ascAPIIntel) IndexStatus(ctx context.Context, req *ascv1.IndexStatusRequest) (*ascv1.IndexStatusResponse, error) {
	f := c.f
	f.mu.Lock()
	f.statusC = append(f.statusC, req)
	f.note(ctx)
	fn := f.onStatus
	f.mu.Unlock()
	if fn == nil {
		return nil, status.Error(codes.Unimplemented, "x")
	}
	return fn(req)
}

// useAscAPI starts the fake, connects the process to it and turns the integration on.
func useAscAPI(t *testing.T, f *ascAPIFake) {
	t.Helper()
	srv := grpc.NewServer()
	ascv1.RegisterCrossSearchServer(srv, ascAPICross{f: f})
	ascv1.RegisterCodeIntelServer(srv, ascAPIIntel{f: f})
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	go func() { _ = srv.Serve(l) }()
	t.Cleanup(srv.Stop)
	c, err := ascclient.New(ascclient.Config{Target: l.Addr().String(), InternalToken: ascAPIInternal, Retry: ascclient.RetryConfig{MaxAttempts: 1}})
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Close() })
	t.Cleanup(ascsearch.UseConn(c))
	t.Cleanup(test.MockVariableValue(&setting.Asc.Enabled, true))
}

func setAscBackend(t *testing.T, ids ...int64) {
	t.Helper()
	for _, id := range ids {
		_, err := db.GetEngine(db.DefaultContext).ID(id).Cols("storage_backend").Update(&repo_model.Repository{StorageBackend: "asc"})
		require.NoError(t, err)
	}
}

type ascCall struct {
	userID  int64 // 0 is anonymous
	auth    string
	reducer authz.AuthorizationReducer
	repoID  int64
}

// run calls a handler as the user with the given token; a handler of a repository route gets repoID loaded.
func (c ascCall) run(t *testing.T, h func(*api_context.APIContext), target string) (int, string) {
	t.Helper()
	ctx, resp := contexttest.MockAPIContext(t, target)
	if c.userID != 0 {
		contexttest.LoadUser(t, ctx, c.userID)
		ctx.SetIsSigned(true)
	}
	if c.repoID != 0 {
		contexttest.LoadRepo(t, ctx, c.repoID)
	}
	if c.reducer != nil {
		ctx.SetReducer(c.reducer)
	}
	if c.auth != "" {
		ctx.Req.Header.Set("Authorization", c.auth)
	}
	h(ctx)
	return resp.Code, resp.Body.String()
}

func user(id int64, token string) ascCall {
	return ascCall{userID: id, auth: "Bearer " + token, reducer: &authz.AllAccessAuthorizationReducer{}}
}

func ascRef(name string) *ascv1.RepoRef { return &ascv1.RepoRef{Name: name} }

func ascHit(path, text string) *ascv1.FileHit {
	return &ascv1.FileHit{Path: []byte(path), Lang: "Go", Matches: []*ascv1.LineMatch{{Line: 3, Cols: []uint32{1}, Text: []byte(text)}}}
}

func decode[T any](t *testing.T, body string) T {
	t.Helper()
	var v T
	require.NoError(t, json.Unmarshal([]byte(body), &v), body)
	return v
}

func TestAscCrossSearchAPI(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	setAscBackend(t, 1, 2, 3, 4)
	const key = "AKIAIOSFODNN7EXAMPLE"
	f := &ascAPIFake{onCross: func(req *ascv1.CrossSearchRequest) ([]*ascv1.CrossSearchResponse, error) {
		var hits []*ascv1.RepoHits
		for _, n := range req.GetTarget().GetRepos() {
			hits = append(hits, &ascv1.RepoHits{Repo: ascRef(n), Rev: "master", Files: []*ascv1.FileHit{
				ascHit("src/a.go", "token \x1b[31m := 1"),
				{Path: []byte("conf.env"), Redacted: true, Matches: []*ascv1.LineMatch{{Line: 1, Text: []byte("K=" + key)}}},
			}})
		}
		hits = append(hits, &ascv1.RepoHits{Repo: ascRef("secret/private"), Files: []*ascv1.FileHit{ascHit("leak.go", "leaked")}})
		return []*ascv1.CrossSearchResponse{{Repos: hits, SearchedRepos: 2, UnindexedRepos: 1, NextPageToken: "tok1", Truncated: true, EstTokens: 7}}, nil
	}}
	useAscAPI(t, f)

	t.Run("a search names the readable repositories and carries the caller's token", func(t *testing.T) {
		code, body := user(2, "user-token-2").run(t, AscCrossSearch,
			"/api/v1/asc/search?q=token&mode=regex&case_sensitive=true&context=2&repo=user2/repo1&repo=user2/repo2&path=src&exclude=vendor&lang=Go&limit=9&page_token=p0")
		require.Equal(t, http.StatusOK, code, body)
		require.Len(t, f.cross, 1)
		req := f.cross[0]
		assert.ElementsMatch(t, []string{"user2/repo1", "user2/repo2"}, req.GetTarget().GetRepos())
		assert.Nil(t, req.GetTarget().GetScope(), "never an instance scope: Forgejo names the repositories")
		assert.Equal(t, ascv1.SearchMode_SEARCH_MODE_REGEX, req.GetMode())
		assert.True(t, req.GetCaseSensitive())
		assert.EqualValues(t, 2, req.GetContextLines())
		assert.Equal(t, []string{"src"}, req.GetPaths())
		assert.Equal(t, []string{"vendor"}, req.GetExcludePaths())
		assert.Equal(t, []string{"Go"}, req.GetLangs())
		assert.EqualValues(t, 9, req.GetBudget().GetMaxResults())
		assert.Equal(t, "p0", req.GetPageToken())
		assert.Equal(t, []string{"Bearer user-token-2"}, f.auth, "the user's own token, never the internal one")
		assert.NotContains(t, body, ascAPIInternal)

		res := decode[api.AscCrossSearchResult](t, body)
		require.Len(t, res.Repos, 2)
		assert.Equal(t, 4, res.Files)
		assert.True(t, res.Truncated)
		assert.Equal(t, "tok1", res.NextPageToken)
		assert.Equal(t, 2, res.SearchedRepos)
		assert.Equal(t, 1, res.UnindexedRepos)
		assert.EqualValues(t, 7, res.EstTokens)
		assert.Equal(t, 2, res.ReposTotal)
		assert.False(t, res.ReposTruncated)
		assert.Equal(t, []api.AscFacet{{Name: "Go", Count: 2}}, res.Languages)
		assert.Equal(t, []api.AscFacet{{Name: "(root)", Count: 2}, {Name: "src/", Count: 2}}, res.Directories)
		assert.Equal(t, "token [31m := 1", res.Repos[0].Files[0].Matches[0].Text, "control characters are gone")
		// A repository nobody asked for and a redacted line never reach the client.
		assert.NotContains(t, body, "leak")
		assert.NotContains(t, body, "secret/private")
		assert.NotContains(t, body, key)
		assert.True(t, res.Repos[0].Files[1].Redacted)
	})

	t.Run("a repository the caller cannot read leaves no trace", func(t *testing.T) {
		f.cross = nil
		code, body := user(5, "user-token-5").run(t, AscCrossSearch, "/api/v1/asc/search?q=token&repo=user2/repo2&repo=user2/repo1&repo=nobody/none")
		require.Equal(t, http.StatusOK, code, body)
		assert.Equal(t, []string{"user2/repo1"}, f.cross[0].GetTarget().GetRepos())
		assert.NotContains(t, body, "repo2")

		f.cross = nil
		code, _ = user(5, "user-token-5").run(t, AscCrossSearch, "/api/v1/asc/search?q=token&repo=user2/repo2")
		assert.Equal(t, http.StatusNotFound, code, "the same answer as for a repository that does not exist")
		code, _ = user(5, "user-token-5").run(t, AscCrossSearch, "/api/v1/asc/search?q=token&repo=nobody/none")
		assert.Equal(t, http.StatusNotFound, code)
		assert.Empty(t, f.cross, "asc is not even asked")
	})

	t.Run("without repo or owner every readable repository is searched", func(t *testing.T) {
		f.cross = nil
		code, body := user(5, "user-token-5").run(t, AscCrossSearch, "/api/v1/asc/search?q=token")
		require.Equal(t, http.StatusOK, code, body)
		assert.ElementsMatch(t, []string{"user2/repo1", "user5/repo4"}, f.cross[0].GetTarget().GetRepos())

		f.cross = nil
		code, _ = user(2, "user-token-2").run(t, AscCrossSearch, "/api/v1/asc/search?q=token&owner=user2")
		require.Equal(t, http.StatusOK, code)
		assert.ElementsMatch(t, []string{"user2/repo1", "user2/repo2"}, f.cross[0].GetTarget().GetRepos())

		f.cross = nil
		code, body = user(2, "user-token-2").run(t, AscCrossSearch, "/api/v1/asc/search?q=token&owner=nobody")
		require.Equal(t, http.StatusOK, code, body)
		assert.Empty(t, f.cross, "no repository, no call")
		assert.Contains(t, body, `"repos":[]`)
	})

	t.Run("a token limited to public repositories searches public repositories only", func(t *testing.T) {
		f.cross = nil
		c := user(2, "user-token-2")
		c.reducer = &authz.PublicReposAuthorizationReducer{}
		code, body := c.run(t, AscCrossSearch, "/api/v1/asc/search?q=token")
		require.Equal(t, http.StatusOK, code, body)
		assert.ElementsMatch(t, []string{"user2/repo1", "user5/repo4"}, f.cross[0].GetTarget().GetRepos())
		code, _ = c.run(t, AscCrossSearch, "/api/v1/asc/search?q=token&repo=user2/repo2")
		assert.Equal(t, http.StatusNotFound, code)
	})
}

func TestAscCrossSearchAPIChecks(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	setAscBackend(t, 1, 2)
	f := &ascAPIFake{onCross: func(*ascv1.CrossSearchRequest) ([]*ascv1.CrossSearchResponse, error) { return nil, nil }}
	useAscAPI(t, f)
	u := user(2, "user-token-2")

	for target, want := range map[string]string{
		"/api/v1/asc/search?q=ab":                                   "the pattern must be 3 to 512 bytes",
		"/api/v1/asc/search?q=" + strings.Repeat("a", 513):          "the pattern must be 3 to 512 bytes",
		"/api/v1/asc/search?q=abc&mode=nonsense":                    "bad mode",
		"/api/v1/asc/search?q=abc&context=9":                        "bad context",
		"/api/v1/asc/search?q=abc&context=-1":                       "bad context",
		"/api/v1/asc/search?q=abc&path=../x":                        "bad path",
		"/api/v1/asc/search?q=abc&lang=a%3Bb":                       "bad language",
		"/api/v1/asc/search?q=abc&page_token=a%20b":                 "bad page token",
		"/api/v1/asc/search?q=abc&repo=nodash":                      "bad repository or owner name",
		"/api/v1/asc/search?q=abc&owner=a/b":                        "bad repository or owner name",
		"/api/v1/asc/symbols?name=":                                 "the name must be 1 to 256 bytes",
		"/api/v1/asc/symbols?name=a&match=sounds-like":              "bad match",
		"/api/v1/asc/semantic?q=":                                   "the question must be 1 to 1000 bytes",
		"/api/v1/asc/semantic?q=" + strings.Repeat("a", 1001):       "the question must be 1 to 1000 bytes",
		"/api/v1/asc/search?q=abc&repo=" + strings.Repeat("a", 300): "bad repository or owner name",
	} {
		h := AscCrossSearch
		switch {
		case strings.Contains(target, "/symbols"):
			h = AscCrossSymbols
		case strings.Contains(target, "/semantic"):
			h = AscCrossSemantic
		}
		code, body := u.run(t, h, target)
		assert.Equal(t, http.StatusUnprocessableEntity, code, target)
		assert.Contains(t, body, want, target)
	}
	assert.Empty(t, f.cross, "nothing bad reaches asc")

	t.Run("a token and a signed-in user are required", func(t *testing.T) {
		for name, c := range map[string]ascCall{
			"no token":      {userID: 2, reducer: &authz.AllAccessAuthorizationReducer{}},
			"bad token":     {userID: 2, auth: "Bearer bad token", reducer: &authz.AllAccessAuthorizationReducer{}},
			"anonymous":     {auth: "Bearer user-token-2", reducer: &authz.AllAccessAuthorizationReducer{}},
			"not a scheme":  {userID: 2, auth: "user-token-2", reducer: &authz.AllAccessAuthorizationReducer{}},
			"empty bearer":  {userID: 2, auth: "Bearer ", reducer: &authz.AllAccessAuthorizationReducer{}},
			"basic w/o pwd": {userID: 2, auth: "Basic dXNlcjo=", reducer: &authz.AllAccessAuthorizationReducer{}},
		} {
			code, _ := c.run(t, AscCrossSearch, "/api/v1/asc/search?q=abc")
			assert.Equal(t, http.StatusUnauthorized, code, name)
		}
		assert.Empty(t, f.cross)
	})

	t.Run("without a reducer the limits of the token are unknown, so nothing is searched", func(t *testing.T) {
		code, _ := ascCall{userID: 2, auth: "Bearer user-token-2"}.run(t, AscCrossSearch, "/api/v1/asc/search?q=abc")
		assert.Equal(t, http.StatusInternalServerError, code)
		assert.Empty(t, f.cross)
	})

	t.Run("off while the integration is off", func(t *testing.T) {
		defer test.MockVariableValue(&setting.Asc.Enabled, false)()
		code, _ := u.run(t, AscCrossSearch, "/api/v1/asc/search?q=abc")
		assert.Equal(t, http.StatusNotImplemented, code)
		code, _ = u.run(t, AscCrossSymbols, "/api/v1/asc/symbols?name=abc")
		assert.Equal(t, http.StatusNotImplemented, code)
		code, _ = u.run(t, AscCrossSemantic, "/api/v1/asc/semantic?q=abc")
		assert.Equal(t, http.StatusNotImplemented, code)
	})
}

func TestAscCrossSearchAPIErrors(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	setAscBackend(t, 1)
	f := &ascAPIFake{}
	useAscAPI(t, f)
	u := user(2, "user-token-2")
	tooBroad := func() error {
		st, _ := status.New(codes.InvalidArgument, "SECRET-DETAIL").WithDetails(&ascv1.ErrorDetail{Code: "SEARCH_TOO_BROAD"})
		return st.Err()
	}

	for name, c := range map[string]struct {
		err  error
		code int
		msg  string
	}{
		"internal":  {status.Error(codes.Internal, "SECRET-DETAIL /srv/path"), http.StatusBadGateway, "code search failed"},
		"limited":   {status.Error(codes.ResourceExhausted, "SECRET-DETAIL"), http.StatusTooManyRequests, "too many requests"},
		"down":      {status.Error(codes.Unavailable, "SECRET-DETAIL"), http.StatusServiceUnavailable, "unavailable"},
		"slow":      {status.Error(codes.DeadlineExceeded, "SECRET-DETAIL"), http.StatusGatewayTimeout, "too long"},
		"too broad": {tooBroad(), http.StatusUnprocessableEntity, "too broad"},
		"refused":   {status.Error(codes.InvalidArgument, "SECRET-DETAIL"), http.StatusUnprocessableEntity, "refused"},
	} {
		f.onCross = func(*ascv1.CrossSearchRequest) ([]*ascv1.CrossSearchResponse, error) { return nil, c.err }
		code, body := u.run(t, AscCrossSearch, "/api/v1/asc/search?q=abc")
		assert.Equal(t, c.code, code, name)
		assert.Contains(t, body, c.msg, name)
		assert.NotContains(t, body, "SECRET-DETAIL", name)
		assert.NotContains(t, body, "/srv/path", name)
	}
}

func TestAscCrossSymbolsAndSemanticAPI(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	setAscBackend(t, 1, 2, 4)
	sym := func(name, path string, def bool) *ascv1.Symbol {
		return &ascv1.Symbol{Name: name, Kind: "fn", IsDef: def, Path: []byte(path), Line: 3, EndLine: 5, Confidence: "precise"}
	}
	f := &ascAPIFake{
		onLookup: func(req *ascv1.LookupSymbolRequest) (*ascv1.LookupSymbolResponse, error) {
			return &ascv1.LookupSymbolResponse{Hits: []*ascv1.SymbolHit{
				{Repo: ascRef("user2/repo1"), Rev: "master", Symbol: sym("Retry", "a.go", true), Score: 1},
				{Repo: ascRef("hidden/repo"), Symbol: sym("Retry", "b.go", true), Score: 1},
			}, NextPageToken: "n1"}, nil
		},
		onSemantic: func(req *ascv1.SemanticRequest) (*ascv1.SemanticResponse, error) {
			if req.GetScope().GetRepo().GetName() == "user5/repo4" {
				return nil, status.Error(codes.PermissionDenied, "x")
			}
			return &ascv1.SemanticResponse{Model: "m", Chunks: []*ascv1.Chunk{
				{Path: []byte("a.go"), StartLine: 1, EndLine: 4, Score: 0.9, Text: []byte("code")},
				{Path: []byte("s.env"), Redacted: true, Text: []byte("AKIAIOSFODNN7EXAMPLE")},
			}}, nil
		},
	}
	useAscAPI(t, f)
	u := user(2, "user-token-2")

	code, body := u.run(t, AscCrossSymbols, "/api/v1/asc/symbols?name=Retry&match=prefix&kind=fn&lang=Go&definitions_only=true&limit=7&repo=user2/repo1")
	require.Equal(t, http.StatusOK, code, body)
	req := f.look[0]
	assert.Equal(t, []string{"user2/repo1"}, req.GetTarget().GetRepos())
	assert.Equal(t, ascv1.SymbolMatch_SYMBOL_MATCH_PREFIX, req.GetMatch())
	assert.True(t, req.GetDefinitionsOnly())
	assert.EqualValues(t, 7, req.GetLimit())
	assert.Equal(t, []string{"Bearer user-token-2"}, f.auth)
	res := decode[api.AscCrossSymbolsResult](t, body)
	require.Len(t, res.Symbols, 1, "a repository nobody asked for is dropped")
	assert.Equal(t, "user2/repo1", res.Symbols[0].Repo)
	assert.Equal(t, "n1", res.NextPageToken)
	assert.NotContains(t, body, "hidden")

	f.auth = nil
	code, body = u.run(t, AscCrossSemantic, "/api/v1/asc/semantic?q=where+do+we+retry&k=3&rerank=true&path=src&lang=Go&repo=user2/repo1&repo=user5/repo4")
	require.Equal(t, http.StatusOK, code, body)
	require.Len(t, f.sem, 2, "one query embedding per repository")
	for _, r := range f.sem {
		assert.EqualValues(t, 3, r.GetK())
		assert.True(t, r.GetRerank())
		assert.Equal(t, []string{"src"}, r.GetScope().GetPaths())
	}
	assert.Equal(t, []string{"Bearer user-token-2", "Bearer user-token-2"}, f.auth)
	sem := decode[api.AscCrossSemanticResult](t, body)
	assert.Equal(t, 1, sem.SearchedRepos, "the refused repository adds nothing")
	assert.Zero(t, sem.FailedRepos)
	require.Len(t, sem.Chunks, 2)
	assert.True(t, sem.Chunks[1].Redacted)
	assert.Empty(t, sem.Chunks[1].Text)
	assert.NotContains(t, body, "AKIA")
}

func TestAscSymbolPanelAPI(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	setAscBackend(t, 1, 2, 4)
	sym := func(name, path string, def bool) *ascv1.Symbol {
		return &ascv1.Symbol{Name: name, Kind: "fn", IsDef: def, Path: []byte(path), Line: 3, EndLine: 5, Confidence: "name_match"}
	}
	refs := func(repo, path string) *ascv1.SymbolReferences {
		return &ascv1.SymbolReferences{Repo: ascRef(repo), Path: []byte(path), References: []*ascv1.Symbol{sym("Retry", path, false)}}
	}
	f := &ascAPIFake{onPanel: func(req *ascv1.SymbolPanelRequest) (*ascv1.SymbolPanelResponse, error) {
		return &ascv1.SymbolPanelResponse{
			Scope:       &ascv1.ResolvedScope{Coverage: 1},
			Definitions: []*ascv1.SymbolHit{{Repo: ascRef("user2/repo1"), Symbol: sym("Retry", "a.go", true), Score: 1}},
			References:  []*ascv1.SymbolReferences{refs("user2/repo1", "b.go")},
			Callers:     []*ascv1.SymbolReferences{refs("user2/repo1", "c.go")},
			CrossRepo:   []*ascv1.SymbolReferences{refs("user5/repo4", "use.go"), refs("user2/repo2", "private.go")},
		}, nil
	}}
	useAscAPI(t, f)

	t.Run("references in other repositories are limited to the ones the caller can read", func(t *testing.T) {
		c := user(5, "user-token-5")
		c.repoID = 1
		code, body := c.run(t, AscSymbolPanel, "/api/v1/repos/user2/repo1/asc/symbol-panel?name=Retry&path=a.go&line=3&cross_repo=true&rev=master")
		require.Equal(t, http.StatusOK, code, body)
		req := f.panel[0]
		assert.Equal(t, "user2/repo1", req.GetRepo().GetName())
		assert.Equal(t, "Retry", req.GetName())
		assert.Equal(t, "master", req.GetRev())
		assert.True(t, req.GetCrossRepo())
		assert.Equal(t, []string{"Bearer user-token-5"}, f.auth)
		panel := decode[api.AscSymbolPanel](t, body)
		require.Len(t, panel.Definitions, 1)
		require.Len(t, panel.References, 1)
		require.Len(t, panel.Callers, 1)
		require.Len(t, panel.CrossRepo, 1)
		assert.Equal(t, "user5/repo4", panel.CrossRepo[0].Repo)
		assert.NotContains(t, body, "private.go", "user5 cannot read user2/repo2")
	})

	t.Run("without cross_repo no other repository is asked for", func(t *testing.T) {
		c := user(2, "user-token-2")
		c.repoID = 1
		code, body := c.run(t, AscSymbolPanel, "/api/v1/repos/user2/repo1/asc/symbol-panel?name=Retry")
		require.Equal(t, http.StatusOK, code, body)
		assert.False(t, f.panel[len(f.panel)-1].GetCrossRepo())
		assert.Empty(t, decode[api.AscSymbolPanel](t, body).CrossRepo)
		assert.Contains(t, body, `"cross_repo":[]`)
	})

	t.Run("checks", func(t *testing.T) {
		c := user(2, "user-token-2")
		c.repoID = 1
		for target, want := range map[string]int{
			"/x?name=":                      http.StatusUnprocessableEntity,
			"/x?name=a&line=-1":             http.StatusUnprocessableEntity,
			"/x?name=a&path=../b":           http.StatusUnprocessableEntity,
			"/x?name=a&change=kkkkkkkkkkkk": http.StatusUnprocessableEntity,
			"/x?name=a&rev=-x":              http.StatusUnprocessableEntity,
		} {
			code, _ := c.run(t, AscSymbolPanel, target)
			assert.Equal(t, want, code, target)
		}
		// A repository outside asc has no code index.
		c.repoID = 3
		code, _ := c.run(t, AscSymbolPanel, "/x?name=a")
		assert.Equal(t, http.StatusNotFound, code)
		// An unreadable repository is not found.
		other := user(5, "user-token-5")
		other.repoID = 2
		code, _ = other.run(t, AscSymbolPanel, "/x?name=a")
		assert.Equal(t, http.StatusNotFound, code)
		// No token.
		anon := ascCall{userID: 2, reducer: &authz.AllAccessAuthorizationReducer{}, repoID: 1}
		code, _ = anon.run(t, AscSymbolPanel, "/x?name=a")
		assert.Equal(t, http.StatusUnauthorized, code)
	})

	t.Run("a server without the panel service falls back to symbol calls", func(t *testing.T) {
		f.onPanel = nil
		code, body := user(2, "user-token-2").withRepo(1).run(t, AscSymbolPanel, "/x?name=Retry")
		// Symbols is not implemented by the fake either, so the whole panel is unavailable.
		assert.Equal(t, http.StatusServiceUnavailable, code, body)
		assert.NotContains(t, body, "Unimplemented")

		f.onSymbols = func(req *ascv1.SymbolsRequest) (*ascv1.SymbolsResponse, error) {
			switch req.GetOp() {
			case ascv1.SymbolOp_SYMBOL_OP_DEF:
				return &ascv1.SymbolsResponse{Symbols: []*ascv1.Symbol{sym("Retry", "a.go", true)}}, nil
			case ascv1.SymbolOp_SYMBOL_OP_REFS:
				return &ascv1.SymbolsResponse{Symbols: []*ascv1.Symbol{sym("Retry", "b.go", false)}}, nil
			}
			return &ascv1.SymbolsResponse{}, nil
		}
		code, body = user(2, "user-token-2").withRepo(1).run(t, AscSymbolPanel, "/x?name=Retry")
		require.Equal(t, http.StatusOK, code, body)
		panel := decode[api.AscSymbolPanel](t, body)
		require.Len(t, panel.Definitions, 1)
		require.Len(t, panel.References, 1)
		assert.Equal(t, "b.go", panel.References[0].Path)
		assert.Empty(t, panel.Callers)
		assert.Empty(t, panel.CrossRepo)
	})
}

func (c ascCall) withRepo(id int64) ascCall {
	c.repoID = id
	return c
}

func TestAscIndexStatusAPI(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	setAscBackend(t, 1)
	f := &ascAPIFake{onStatus: func(*ascv1.IndexStatusRequest) (*ascv1.IndexStatusResponse, error) {
		return &ascv1.IndexStatusResponse{
			Scope: &ascv1.ResolvedScope{Changeset: []byte{0xe9, 0x1b, 0x0d, 0x44}},
			Indexes: []*ascv1.IndexStatus{
				{Index: "files", Coverage: 0.992, LagSeconds: 4},
				{Index: "vectors", Coverage: 0.5, LagSeconds: 90},
			},
		}, nil
	}}
	useAscAPI(t, f)

	code, body := user(2, "user-token-2").withRepo(1).run(t, AscIndexStatus, "/api/v1/repos/user2/repo1/asc/index")
	require.Equal(t, http.StatusOK, code, body)
	res := decode[api.AscIndexStatusResult](t, body)
	assert.Equal(t, "user2/repo1", res.Repo)
	assert.Equal(t, "master", res.Rev)
	assert.Equal(t, 99, res.Files.Percent)
	assert.False(t, res.Files.Complete)
	assert.EqualValues(t, 4, res.Files.LagSeconds)
	assert.Equal(t, "e91b0d4", res.Files.Revision)
	assert.Equal(t, 50, res.Vectors.Percent)
	assert.Equal(t, []string{"Bearer user-token-2", "Bearer user-token-2"}, f.auth)

	// No answer at all is a failure, not "zero percent".
	f.onStatus = nil
	code, body = user(2, "user-token-2").withRepo(1).run(t, AscIndexStatus, "/x")
	assert.Equal(t, http.StatusBadGateway, code, body)

	code, _ = ascCall{userID: 2, reducer: &authz.AllAccessAuthorizationReducer{}, repoID: 1}.run(t, AscIndexStatus, "/x")
	assert.Equal(t, http.StatusUnauthorized, code)
	code, _ = user(2, "user-token-2").withRepo(3).run(t, AscIndexStatus, "/x")
	assert.Equal(t, http.StatusNotFound, code, "not an asc repository")
}

func TestAscCodeintelChangeScope(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	f := &ascAPIFake{}
	useAscAPI(t, f)
	t.Cleanup(func() { SetAscCodeIntel(nil) })
	fake := &ascCIFake{search: &ascv1.SearchResponse{}}
	SetAscCodeIntel(fake)
	const change = "kkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkk"

	code, body := ascCICall(t, AscSearch, "user2/repo1/asc/search?q=abc&change="+change, "Bearer user-token-1")
	require.Equal(t, http.StatusOK, code, body)
	sc := fake.lastReq.(*ascv1.SearchRequest).GetScope()
	assert.Len(t, sc.GetChange(), 16)
	assert.Nil(t, sc.GetStackTop())
	assert.Empty(t, sc.GetRev())
	assert.Equal(t, "user2/repo1", sc.GetRepo().GetName())

	code, body = ascCICall(t, AscSearch, "user2/repo1/asc/search?q=abc&stack="+change+"&stack_diff_only=true", "Bearer user-token-1")
	require.Equal(t, http.StatusOK, code, body)
	sc = fake.lastReq.(*ascv1.SearchRequest).GetScope()
	assert.Len(t, sc.GetStackTop(), 16)
	assert.True(t, sc.GetStackDiffOnly())

	for _, bad := range []string{
		"change=" + change + "&rev=master", "change=" + change + "&stack=" + change, "stack=" + change + "&rev=master",
		"change=nope", "stack=" + change[:31], "change=" + strings.ToUpper(change),
	} {
		code, _ = ascCICall(t, AscSearch, "user2/repo1/asc/search?q=abc&"+bad, "Bearer user-token-1")
		assert.Equal(t, http.StatusUnprocessableEntity, code, bad)
	}

	fake.symbols, fake.semantic = &ascv1.SymbolsResponse{}, &ascv1.SemanticResponse{}
	code, body = ascCICall(t, AscSymbols, "user2/repo1/asc/symbols?name=x&change="+change, "Bearer user-token-1")
	require.Equal(t, http.StatusOK, code, body)
	assert.Len(t, fake.lastReq.(*ascv1.SymbolsRequest).GetScope().GetChange(), 16)
	code, body = ascCICall(t, AscSemantic, "user2/repo1/asc/semantic?q=x&stack="+change, "Bearer user-token-1")
	require.Equal(t, http.StatusOK, code, body)
	assert.Len(t, fake.lastReq.(*ascv1.SemanticRequest).GetScope().GetStackTop(), 16)
}

func TestAscCodeintelDefaultConnection(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	SetAscCodeIntel(nil)
	f := &ascAPIFake{}
	useAscAPI(t, f)
	f.onStatus = func(*ascv1.IndexStatusRequest) (*ascv1.IndexStatusResponse, error) {
		return &ascv1.IndexStatusResponse{Indexes: []*ascv1.IndexStatus{{Index: "files", Coverage: 1}}}, nil
	}
	// The per-repository routes use the connection to asc serve when no client was set.
	code, body := ascCICall(t, AscSearch, "user2/repo1/asc/search?q=abc", "Bearer user-token-1")
	require.Equal(t, http.StatusOK, code, body)
	require.Len(t, f.srch, 1)
	assert.Equal(t, []string{"Bearer user-token-1"}, f.auth)

	defer test.MockVariableValue(&setting.Asc.Enabled, false)()
	restore := ascsearch.UseConn(nil)
	defer restore()
	code, _ = ascCICall(t, AscSearch, "user2/repo1/asc/search?q=abc", "Bearer user-token-1")
	assert.Equal(t, http.StatusNotImplemented, code)
}
