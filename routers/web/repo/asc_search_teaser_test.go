// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	"net/http"
	"testing"

	"forgejo.org/models/unittest"
	ascv1 "forgejo.org/modules/ascproto/ascv1"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestAscSearchTeasers(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	setStorageBackend(t, 1, "asc")
	fake := startFakeSearch(t, &fakeSearchServer{
		search: func(*ascv1.SearchRequest) ([]*ascv1.SearchResponse, error) {
			return []*ascv1.SearchResponse{{Files: []*ascv1.FileHit{fileHit("src/a.rs", "aa", "retry"), fileHit("src/b.rs", "bb", "retry")}, Truncated: true}}, nil
		},
		symbols: func(req *ascv1.SymbolsRequest) (*ascv1.SymbolsResponse, error) {
			if req.GetOp() != ascv1.SymbolOp_SYMBOL_OP_FIND {
				return &ascv1.SymbolsResponse{}, nil
			}
			return &ascv1.SymbolsResponse{Symbols: []*ascv1.Symbol{ascSym("retry", "src/r.rs", 3, true, "precise")}}, nil
		},
		semantic: func(*ascv1.SemanticRequest) (*ascv1.SemanticResponse, error) {
			return &ascv1.SemanticResponse{}, nil
		},
	})

	page := getAscSearch(t, "?q=retry&match=regex")
	require.Equal(t, http.StatusOK, page.status, page.body)
	assert.Contains(t, page.body, `data-testid="search-teasers"`)
	assert.Contains(t, page.body, `href="/user2/repo1/asc/search?mode=symbol&amp;q=retry&amp;scope=version">asc_rp.s_symbol</a>`, "the text options are dropped")
	assert.Contains(t, page.body, `<span class="asc-count">1</span>`)
	assert.Contains(t, page.body, `href="/user2/repo1/src/branch/master/src/r.rs#L3">retry</a>`)
	assert.Contains(t, page.body, `href="/user2/repo1/asc/search?mode=semantic&amp;q=retry&amp;scope=version">asc_rp.s_meaning</a>`)
	assert.Contains(t, page.body, "asc_rp.s_teaser_meaning")
	fake.mu.Lock()
	sems := len(fake.sems)
	fake.mu.Unlock()
	assert.Zero(t, sems, "a text search never pays for a meaning search")

	page = getAscSearch(t, "?q=retry&mode=semantic")
	require.Equal(t, http.StatusOK, page.status, page.body)
	assert.Contains(t, page.body, `asc_rp.s_text</a>`)
	assert.Contains(t, page.body, `<span class="asc-count">2+</span>`)
	assert.Contains(t, page.body, `asc_rp.s_symbol</a>`)
	assert.NotContains(t, page.body, "asc_rp.s_teaser_meaning")

	t.Run("a failed or empty lookup hides its line", func(t *testing.T) {
		fake.symbols = func(*ascv1.SymbolsRequest) (*ascv1.SymbolsResponse, error) {
			return nil, status.Error(codes.Unavailable, "down")
		}
		fake.search = func(*ascv1.SearchRequest) ([]*ascv1.SearchResponse, error) {
			return []*ascv1.SearchResponse{{}}, nil
		}
		page := getAscSearch(t, "?q=retry&mode=semantic")
		require.Equal(t, http.StatusOK, page.status, page.body)
		assert.NotContains(t, page.body, `data-testid="search-teasers"`)
	})
}
