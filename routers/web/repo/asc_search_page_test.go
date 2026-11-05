// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"forgejo.org/models/unittest"
	ascv1 "forgejo.org/modules/ascproto/ascv1"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAscSearchPageCut(t *testing.T) {
	q := ascSearchQuery{text: "x", mode: ascSearchModeLexical, match: ascSearchMatchLiteral, scope: ascSearchScopeVersion}
	files := func(n int) *ascSearchResult {
		r := &ascSearchResult{}
		for i := range n {
			r.files = append(r.files, ascSearchFile{Path: fmt.Sprintf("f%02d", i)})
		}
		return r
	}
	r := files(23)
	p := ascSearchPage("/o/r", q, 3, r)
	assert.Equal(t, 21, p.From)
	assert.Equal(t, 23, p.To)
	assert.Equal(t, 23, p.Total)
	assert.Len(t, r.files, 3)
	assert.Equal(t, "f20", r.files[0].Path)
	assert.Len(t, p.Pages, 3)
	assert.Empty(t, p.Pages[2].Link, "the page shown has no link")
	assert.Equal(t, "/o/r/asc/search?match=literal&mode=lexical&page=2&q=x&scope=version", p.Prev)
	assert.Empty(t, p.Next)
	assert.Equal(t, "/o/r/asc/search?match=literal&mode=lexical&q=x&scope=version", p.Pages[0].Link, "page 1 has no page key")

	r = files(23)
	p = ascSearchPage("/o/r", q, 99, r)
	assert.Equal(t, 21, p.From, "a page past the end shows the last")
	r = files(23)
	p = ascSearchPage("/o/r", q, -4, r)
	assert.Equal(t, 1, p.From)
	assert.Len(t, r.files, 10)

	r = files(7)
	p = ascSearchPage("/o/r", q, 2, r)
	assert.Nil(t, p.Pages, "one page has no pager")
	assert.Len(t, r.files, 7)
	r = files(0)
	p = ascSearchPage("/o/r", q, 1, r)
	assert.Zero(t, p.From)

	r = &ascSearchResult{files: []ascSearchFile{{Path: "b"}, {Path: "a"}, {Path: "b2"}}, symbols: []ascSymbolsRow{{Path: "z", Line: 9}, {Path: "z", Line: 2}, {Path: "a", Line: 5}}}
	ascSearchSort(q, r)
	assert.Equal(t, "b", r.files[0].Path, "best match keeps the order asc ranked")
	q.sort = ascSearchSortPath
	ascSearchSort(q, r)
	assert.Equal(t, []string{"a", "b", "b2"}, []string{r.files[0].Path, r.files[1].Path, r.files[2].Path})
	assert.Equal(t, []uint32{5, 2, 9}, []uint32{r.symbols[0].Line, r.symbols[1].Line, r.symbols[2].Line})
	assert.Contains(t, ascSearchURL("/o/r", q, nil), "sort=path")
}

func TestAscSearchPaging(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	setStorageBackend(t, 1, "asc")
	var hits []*ascv1.FileHit
	for i := range 23 {
		hits = append(hits, fileHit(fmt.Sprintf("src/f%02d.rs", 22-i), fmt.Sprintf("%02x", i), "needle"))
	}
	startFakeSearch(t, &fakeSearchServer{
		search: func(*ascv1.SearchRequest) ([]*ascv1.SearchResponse, error) {
			return []*ascv1.SearchResponse{{Files: hits, Truncated: true}}, nil
		},
	})
	page := getAscSearch(t, "?q=needle&page=2&lang=Rust")
	require.Equal(t, http.StatusOK, page.status, page.body)
	assert.Contains(t, page.body, "23 asc_rp.s_result_1", "the count is of the whole answer")
	assert.Contains(t, page.body, `data-testid="search-showing">asc_rp.s_showing_more`)
	assert.Contains(t, page.body, "src/f12.rs", "page 2 starts with the 11th hit")
	assert.NotContains(t, page.body, "src/f22.rs")
	assert.Contains(t, page.body, `<span class="asc-chip active" aria-current="page">2</span>`)
	assert.Contains(t, page.body, `href="/user2/repo1/asc/search?lang=Rust&amp;match=literal&amp;mode=lexical&amp;page=3&amp;q=needle&amp;scope=version" rel="next"`)
	assert.Contains(t, page.body, `data-testid="search-sort"`)
	assert.Contains(t, page.body, `href="/user2/repo1/asc/search?lang=Rust&amp;match=literal&amp;mode=lexical&amp;q=needle&amp;scope=version&amp;sort=path"`, "a sort link starts at page 1")
	i := strings.Index(page.body, `class="asc-search-facets"`)
	require.Positive(t, i)
	assert.Contains(t, page.body[i:strings.Index(page.body, `class="asc-search-results"`)], ">23<", "the facets count the whole answer")

	page = getAscSearch(t, "?q=needle&sort=path")
	require.Equal(t, http.StatusOK, page.status)
	assert.Contains(t, page.body, "src/f00.rs")
	assert.NotContains(t, page.body, "src/f10.rs", "page 1 by path ends at the 10th path")
	assert.Contains(t, page.body, `<input type="hidden" name="sort" value="path">`)
}
