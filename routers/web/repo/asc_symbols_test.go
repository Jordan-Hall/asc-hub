// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	gocontext "context"
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

// fakeSymbolsServer is an asc serve that answers CodeIntel.Symbols.
type fakeSymbolsServer struct {
	ascv1.UnimplementedCodeIntelServer
	mu      sync.Mutex
	reqs    []*ascv1.SymbolsRequest
	auth    [][]string
	respond func(*ascv1.SymbolsRequest) (*ascv1.SymbolsResponse, error)
}

func (f *fakeSymbolsServer) Symbols(ctx gocontext.Context, req *ascv1.SymbolsRequest) (*ascv1.SymbolsResponse, error) {
	md, _ := metadata.FromIncomingContext(ctx)
	f.mu.Lock()
	f.reqs = append(f.reqs, req)
	f.auth = append(f.auth, md.Get("authorization"))
	f.mu.Unlock()
	return f.respond(req)
}

func startFakeSymbols(t *testing.T, respond func(*ascv1.SymbolsRequest) (*ascv1.SymbolsResponse, error)) *fakeSymbolsServer {
	t.Helper()
	f := &fakeSymbolsServer{respond: respond}
	srv := grpc.NewServer()
	ascv1.RegisterCodeIntelServer(srv, f)
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	go func() { _ = srv.Serve(l) }()
	t.Cleanup(srv.Stop)
	useAsc(t, l.Addr().String())
	return f
}

func (f *fakeSymbolsServer) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.reqs)
}

func (f *fakeSymbolsServer) all() []*ascv1.SymbolsRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]*ascv1.SymbolsRequest(nil), f.reqs...)
}

func getAscSymbols(t *testing.T, query string) ascGraphPage {
	t.Helper()
	ctx, resp := contexttest.MockContext(t, "/user2/repo1/asc/symbols"+query)
	ctx.Render = templates.HTMLRenderer()
	contexttest.LoadRepo(t, ctx, 1)
	ctx.Data["RepoLink"] = ctx.Repo.RepoLink
	AscSymbols(ctx)
	return ascGraphPage{status: resp.Code, body: resp.Body.String(), ctx: ctx}
}

func ascSym(name, path string, line uint32, def bool, conf string) *ascv1.Symbol {
	return &ascv1.Symbol{Name: name, Kind: "function", IsDef: def, Path: []byte(path), Line: line, EndLine: line + 2, Confidence: conf}
}

func TestAscSymbolsPage(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	setStorageBackend(t, 1, "asc")
	evil := ascSym("<b>evil</b>", "src/a b/lib.rs", 10, true, "precise")
	evil.Signature = []byte("fn evil(\x1b[31m) ‮")
	evil.Doc = []byte("<script>alert(1)</script>")
	fake := startFakeSymbols(t, func(req *ascv1.SymbolsRequest) (*ascv1.SymbolsResponse, error) {
		switch req.GetOp() {
		case ascv1.SymbolOp_SYMBOL_OP_DEF:
			return &ascv1.SymbolsResponse{Symbols: []*ascv1.Symbol{evil}}, nil
		case ascv1.SymbolOp_SYMBOL_OP_REFS:
			return &ascv1.SymbolsResponse{Symbols: []*ascv1.Symbol{ascSym("run", "src/main.rs", 3, false, "name_match")}, Truncated: true}, nil
		default:
			return &ascv1.SymbolsResponse{Symbols: []*ascv1.Symbol{ascSym("main", "src/main.rs", 1, true, "import_match")}}, nil
		}
	})

	t.Run("definition and references", func(t *testing.T) {
		page := getAscSymbols(t, "?op=def&name=run")
		require.Equal(t, http.StatusOK, page.status, page.body)
		reqs := fake.all()
		require.Len(t, reqs, 2)
		assert.Equal(t, ascv1.SymbolOp_SYMBOL_OP_DEF, reqs[0].GetOp())
		assert.Equal(t, ascv1.SymbolOp_SYMBOL_OP_REFS, reqs[1].GetOp())
		assert.Equal(t, "run", reqs[0].GetName())
		assert.Equal(t, "user2/repo1", reqs[0].GetScope().GetRepo().GetName())
		assert.Equal(t, "refs/heads/master", reqs[0].GetScope().GetRev())
		assert.Equal(t, []string{"Bearer " + ascRPCTestToken}, fake.auth[0])
		assert.Contains(t, page.body, "precise")
		assert.Contains(t, page.body, "name_match")
		assert.Contains(t, page.body, "/user2/repo1/src/branch/master/src/a%20b/lib.rs#L10")
		assert.Contains(t, page.body, "More results exist")
		// Text from asc is escaped, and control and bidirectional characters are gone.
		assert.Contains(t, page.body, "&lt;b&gt;evil&lt;/b&gt;")
		assert.Contains(t, page.body, "&lt;script&gt;alert(1)&lt;/script&gt;")
		assert.NotContains(t, page.body, "<script>alert")
		assert.NotContains(t, page.body, "<b>evil")
		assert.NotContains(t, page.body, "\x1b")
		assert.NotContains(t, page.body, "‮")
	})

	t.Run("outline at a commit", func(t *testing.T) {
		sha := strings.Repeat("ab", 20)
		page := getAscSymbols(t, "?op=outline&path=src/main.rs&ref="+sha)
		require.Equal(t, http.StatusOK, page.status, page.body)
		req := fake.all()[fake.count()-1]
		assert.Equal(t, ascv1.SymbolOp_SYMBOL_OP_OUTLINE, req.GetOp())
		assert.Equal(t, "src/main.rs", string(req.GetPath()))
		assert.Equal(t, sha, req.GetScope().GetRev())
		assert.Contains(t, page.body, "/user2/repo1/src/commit/"+sha+"/src/main.rs#L1")
	})

	t.Run("find usages in the base reads the base branch of the pull request", func(t *testing.T) {
		// Pull request 3 of repo 1 goes from branch2 to master.
		before := fake.count()
		page := getAscSymbols(t, "?op=usages&pull=3&name=run+walk&ref=branch2")
		require.Equal(t, http.StatusOK, page.status, page.body)
		reqs := fake.all()[before:]
		require.Len(t, reqs, 2)
		for i, name := range []string{"run", "walk"} {
			assert.Equal(t, ascv1.SymbolOp_SYMBOL_OP_REFS, reqs[i].GetOp())
			assert.Equal(t, name, reqs[i].GetName())
			assert.Equal(t, "refs/heads/master", reqs[i].GetScope().GetRev(), "scope.rev is the base, the ref parameter is ignored")
		}
	})
}

// TestAscSymbolsScopeIsNeverFromTheBrowser: the call carries the internal token, so the scope may only be a
// revision Forgejo resolved. A change id, a stack, a workspace or a made-up branch never reaches asc.
func TestAscSymbolsRefusesBadQueries(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	setStorageBackend(t, 1, "asc")
	fake := startFakeSymbols(t, func(*ascv1.SymbolsRequest) (*ascv1.SymbolsResponse, error) {
		return &ascv1.SymbolsResponse{}, nil
	})
	before := fake.count()
	for name, c := range map[string]struct {
		query string
		code  int
	}{
		"no op":                 {"?name=x", http.StatusBadRequest},
		"unknown op":            {"?op=callers&name=x", http.StatusBadRequest},
		"change id as ref":      {"?op=def&name=x&ref=c:0123456789abcdef", http.StatusNotFound},
		"stack as ref":          {"?op=def&name=x&ref=stack:abc", http.StatusNotFound},
		"unknown branch":        {"?op=def&name=x&ref=nosuch", http.StatusNotFound},
		"option as ref":         {"?op=def&name=x&ref=--help", http.StatusBadRequest},
		"revspec as ref":        {"?op=def&name=x&ref=master%00x", http.StatusBadRequest},
		"upper case sha":        {"?op=def&name=x&ref=" + strings.Repeat("AB", 20), http.StatusBadRequest},
		"no name":               {"?op=def", http.StatusBadRequest},
		"two names for def":     {"?op=def&name=a+b", http.StatusBadRequest},
		"too many names":        {"?op=usages&pull=3&name=" + strings.Repeat("a+", 11), http.StatusBadRequest},
		"long name":             {"?op=def&name=" + strings.Repeat("a", ascSymbolsMaxNameBytes+1), http.StatusBadRequest},
		"control in name":       {"?op=def&name=a%00b", http.StatusBadRequest},
		"bidi in name":          {"?op=def&name=a%E2%80%AEb", http.StatusBadRequest},
		"outline without path":  {"?op=outline", http.StatusBadRequest},
		"path escapes":          {"?op=outline&path=../etc/passwd", http.StatusBadRequest},
		"path with dot":         {"?op=outline&path=a/./b", http.StatusBadRequest},
		"path with backslash":   {"?op=outline&path=a%5Cb", http.StatusBadRequest},
		"path with NUL":         {"?op=outline&path=a%00b", http.StatusBadRequest},
		"usages without pull":   {"?op=usages&name=x", http.StatusBadRequest},
		"usages unknown pull":   {"?op=usages&name=x&pull=999", http.StatusNotFound},
		"usages pull of others": {"?op=usages&name=x&pull=-1", http.StatusBadRequest},
	} {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, c.code, getAscSymbols(t, c.query).status)
		})
	}
	assert.Equal(t, before, fake.count(), "nothing reached asc")

	t.Run("not served for a repository outside asc or with asc off", func(t *testing.T) {
		defer func(v bool) { setting.Asc.Enabled = v }(setting.Asc.Enabled)
		setting.Asc.Enabled = false
		assert.Equal(t, http.StatusNotFound, getAscSymbols(t, "?op=def&name=x").status)
		setting.Asc.Enabled = true
		setStorageBackend(t, 1, "git")
		assert.Equal(t, http.StatusNotFound, getAscSymbols(t, "?op=def&name=x").status)
		setStorageBackend(t, 1, "asc")
		assert.Equal(t, before, fake.count())
	})
}

func TestAscSymbolsRefusesBadAnswers(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	setStorageBackend(t, 1, "asc")
	var answer *ascv1.Symbol
	startFakeSymbols(t, func(*ascv1.SymbolsRequest) (*ascv1.SymbolsResponse, error) {
		return &ascv1.SymbolsResponse{Symbols: []*ascv1.Symbol{answer}}, nil
	})
	for name, s := range map[string]*ascv1.Symbol{
		"parent path":        ascSym("x", "../secret", 1, true, "precise"),
		"absolute-looking":   ascSym("x", "a//b", 1, true, "precise"),
		"empty path":         ascSym("x", "", 1, true, "precise"),
		"unknown confidence": ascSym("x", "a.rs", 1, true, "certain"),
		"lines reversed":     {Name: "x", Path: []byte("a.rs"), Line: 9, EndLine: 3, Confidence: "precise"},
		"huge line":          ascSym("x", "a.rs", 1<<30, true, "precise"),
	} {
		t.Run(name, func(t *testing.T) {
			answer = s
			page := getAscSymbols(t, "?op=outline&path=a.rs")
			assert.Equal(t, http.StatusBadGateway, page.status)
			assert.NotContains(t, page.body, "secret")
		})
	}
}

func TestAscSymbolsErrorsAreGeneric(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	setStorageBackend(t, 1, "asc")
	secret := "sqlite error at /var/lib/asc/asc.db password=hunter2"
	var answer error
	startFakeSymbols(t, func(*ascv1.SymbolsRequest) (*ascv1.SymbolsResponse, error) { return nil, answer })
	for name, c := range map[string]struct {
		err  error
		code int
	}{
		"not found":    {status.Error(codes.NotFound, secret), http.StatusNotFound},
		"invalid":      {status.Error(codes.InvalidArgument, secret), http.StatusBadRequest},
		"exhausted":    {status.Error(codes.ResourceExhausted, secret), http.StatusTooManyRequests},
		"internal":     {status.Error(codes.Internal, secret), http.StatusBadGateway},
		"unauthorised": {status.Error(codes.PermissionDenied, secret), http.StatusBadGateway},
	} {
		t.Run(name, func(t *testing.T) {
			answer = c.err
			page := getAscSymbols(t, "?op=outline&path="+url.QueryEscape("a.rs"))
			assert.Equal(t, c.code, page.status)
			assert.NotContains(t, page.body, "hunter2")
			assert.NotContains(t, page.body, "sqlite")
		})
	}
}

func TestAscSymbolsTextIsPrintable(t *testing.T) {
	assert.Equal(t, "a b c", ascSymbolsText([]byte("a\x00b‮c"), 100))
	assert.Equal(t, "ab...", ascSymbolsText([]byte("abcdef"), 2))
	assert.Equal(t, "a�b", ascSymbolsText([]byte("a\xffb"), 100))
}
