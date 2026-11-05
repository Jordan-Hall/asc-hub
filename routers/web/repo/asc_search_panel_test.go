// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	"net/http"
	"testing"

	"forgejo.org/models/unittest"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/templates"
	"forgejo.org/services/contexttest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func getAscPanel(t *testing.T, query string) ascGraphPage {
	t.Helper()
	ctx, resp := contexttest.MockContext(t, "/user2/repo1/asc/search/panel"+query)
	ctx.Render = templates.HTMLRenderer()
	contexttest.LoadRepo(t, ctx, 1)
	ctx.Data["RepoLink"] = ctx.Repo.RepoLink
	AscSearchPanel(ctx)
	return ascGraphPage{status: resp.Code, body: resp.Body.String(), ctx: ctx}
}

func TestAscSearchPanel(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	setStorageBackend(t, 1, "asc")
	def := ascSym("Parse", "src/lib.rs", 4, true, "precise")
	def.Signature = []byte("fn parse(<x>)")
	def.Doc = []byte("<script>alert(1)</script>")
	def.Container = "Reader"
	fake := startFakeSearch(t, &fakeSearchServer{
		symbols: func(req *ascv1.SymbolsRequest) (*ascv1.SymbolsResponse, error) {
			switch req.GetOp() {
			case ascv1.SymbolOp_SYMBOL_OP_DEF:
				if req.GetName() == "Gone" {
					return nil, status.Error(codes.NotFound, "no such symbol")
				}
				return &ascv1.SymbolsResponse{Symbols: []*ascv1.Symbol{def}}, nil
			case ascv1.SymbolOp_SYMBOL_OP_REFS:
				return &ascv1.SymbolsResponse{Symbols: []*ascv1.Symbol{
					ascSym("Parse", "src/a b.rs", 7, false, "precise"), ascSym("Parse", "src/a b.rs", 9, false, "precise"),
					ascSym("Parse", "src/main.rs", 2, false, "name_match"), ascSym("Parse", "src/x.rs", 1, false, "guess"),
				}}, nil
			case ascv1.SymbolOp_SYMBOL_OP_CALLERS:
				return &ascv1.SymbolsResponse{Symbols: []*ascv1.Symbol{ascSym("run", "src/main.rs", 2, false, "precise")}}, nil
			}
			return &ascv1.SymbolsResponse{}, nil
		},
	})

	page := getAscPanel(t, "?name=Parse&at=src/lib.rs&line=4")
	require.Equal(t, http.StatusOK, page.status, page.body)
	assert.Contains(t, page.body, `data-testid="search-panel"`)
	assert.Contains(t, page.body, "fn parse(&lt;x&gt;)")
	assert.Contains(t, page.body, "&lt;script&gt;alert(1)&lt;/script&gt;")
	assert.NotContains(t, page.body, "<script>alert")
	assert.Contains(t, page.body, `href="/user2/repo1/src/branch/master/src/lib.rs#L4"`)
	assert.Contains(t, page.body, "asc_rp.sp_go_def")
	assert.Contains(t, page.body, `href="/user2/repo1/src/branch/master/src/a%20b.rs#L9"`)
	assert.Contains(t, page.body, `<span class="asc-count">3</span>`, "three references, the unknown confidence dropped")
	assert.Contains(t, page.body, "asc_rp.sp_callers")
	assert.Contains(t, page.body, `href="/user2/repo1/src/branch/master/src/main.rs#L2"`)
	assert.NotContains(t, page.body, "src/x.rs")
	assert.Contains(t, page.body, "asc_rp.sp_fallback")
	assert.Contains(t, page.body, `href="/user2/repo1/asc/symbols?name=Parse&amp;op=def"`)

	fake.mu.Lock()
	var def0 *ascv1.SymbolsRequest
	for _, r := range fake.finds {
		if r.GetOp() == ascv1.SymbolOp_SYMBOL_OP_DEF {
			def0 = r
		}
	}
	fake.mu.Unlock()
	require.NotNil(t, def0)
	assert.Equal(t, "src/lib.rs", string(def0.GetDefPath()))
	assert.EqualValues(t, 4, def0.GetDefLine())

	for _, q := range []string{"", "?at=src/lib.rs", "?name=Parse&at=../x", "?name=Parse&line=-1"} {
		page = getAscPanel(t, q)
		assert.Equal(t, http.StatusBadRequest, page.status, q)
		assert.Contains(t, page.body, "asc_rp.sp_bad", q)
	}
	page = getAscPanel(t, "?name=Gone")
	assert.Equal(t, http.StatusNotFound, page.status)
	assert.Contains(t, page.body, "asc_rp.sp_not_found")

	setStorageBackend(t, 1, "git")
	page = getAscPanel(t, "?name=Parse")
	assert.Equal(t, http.StatusNotFound, page.status)
	assert.NotContains(t, page.body, `data-testid="search-panel"`)
}

func TestAscSearchPanelHooks(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	setStorageBackend(t, 1, "asc")
	startFakeSearch(t, &fakeSearchServer{
		symbols: func(*ascv1.SymbolsRequest) (*ascv1.SymbolsResponse, error) {
			return &ascv1.SymbolsResponse{Symbols: []*ascv1.Symbol{ascSym("Parse", "src/lib.rs", 4, true, "precise")}}, nil
		},
	})
	page := getAscSearch(t, "?q=Parse&mode=symbol")
	require.Equal(t, http.StatusOK, page.status)
	assert.Contains(t, page.body, `id="asc-search-panel"`)
	assert.Contains(t, page.body, `data-search-panel="/user2/repo1/asc/search/panel?name=Parse&amp;at=src%2Flib.rs&amp;line=4"`)
}
