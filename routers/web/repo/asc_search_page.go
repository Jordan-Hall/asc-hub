// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	"cmp"
	"slices"
	"strconv"
)

// Sort and pages of the repository search (board Search: "Sort: best match", "Showing 11-20 of 38"). The
// answer is bounded (ascSearchMaxFiles files, ascSearchMaxChunks chunks), so the hub sorts and pages what
// asc returned; the facets count the whole answer, the page shows one slice of it.

const (
	ascSearchPageSize = 10
	// ascSearchSortPath lists the results by path; the default, best match, keeps the order asc ranked.
	ascSearchSortPath = "path"
)

// ascSearchSortLink is one sort choice.
type ascSearchSortLink struct {
	Key    string // best or path, for the label
	Link   string
	Active bool
}

// ascSearchPageLink is one page link; Link is empty for the page shown.
type ascSearchPageLink struct {
	N    int
	Link string
}

// ascSearchPages is what the pager draws.
type ascSearchPages struct {
	From, To, Total int
	More            bool // asc had more than it returned
	Prev, Next      string
	Pages           []ascSearchPageLink
}

// ascSearchSort orders the answer by path when asked; a stable sort keeps the rank among equal paths.
func ascSearchSort(q ascSearchQuery, res *ascSearchResult) {
	if q.sort != ascSearchSortPath {
		return
	}
	slices.SortStableFunc(res.files, func(a, b ascSearchFile) int { return cmp.Compare(a.Path, b.Path) })
	slices.SortStableFunc(res.symbols, func(a, b ascSymbolsRow) int {
		return cmp.Or(cmp.Compare(a.Path, b.Path), cmp.Compare(a.Line, b.Line))
	})
	slices.SortStableFunc(res.chunks, func(a, b ascSearchChunk) int {
		return cmp.Or(cmp.Compare(a.Path, b.Path), cmp.Compare(a.StartLine, b.StartLine))
	})
}

// ascSearchPage cuts the answer to the page asked for, which is clamped to the pages there are, and returns
// the pager. Only one of the lists has rows for a mode, so they are cut alike.
func ascSearchPage(repoLink string, q ascSearchQuery, page int, res *ascSearchResult) ascSearchPages {
	total := len(res.files) + len(res.symbols) + len(res.chunks)
	last := max(1, (total+ascSearchPageSize-1)/ascSearchPageSize)
	page = min(max(page, 1), last)
	lo := (page - 1) * ascSearchPageSize
	cut := func(n int) (int, int) { return min(lo, n), min(lo+ascSearchPageSize, n) }
	a, b := cut(len(res.files))
	res.files = res.files[a:b]
	a, b = cut(len(res.symbols))
	res.symbols = res.symbols[a:b]
	a, b = cut(len(res.chunks))
	res.chunks = res.chunks[a:b]

	p := ascSearchPages{Total: total, More: res.truncated}
	if total > 0 {
		p.From, p.To = lo+1, min(lo+ascSearchPageSize, total)
	}
	if last == 1 {
		return p
	}
	link := func(n int) string {
		s := ""
		if n > 1 {
			s = strconv.Itoa(n)
		}
		return ascSearchURL(repoLink, q, map[string]string{"page": s})
	}
	for n := 1; n <= last; n++ {
		l := ascSearchPageLink{N: n}
		if n != page {
			l.Link = link(n)
		}
		p.Pages = append(p.Pages, l)
	}
	if page > 1 {
		p.Prev = link(page - 1)
	}
	if page < last {
		p.Next = link(page + 1)
	}
	return p
}

// ascSearchSorts returns the sort choices; each starts again at the first page.
func ascSearchSorts(repoLink string, q ascSearchQuery) []ascSearchSortLink {
	return []ascSearchSortLink{
		{Key: "best", Link: ascSearchURL(repoLink, q, map[string]string{"sort": ""}), Active: q.sort != ascSearchSortPath},
		{Key: "path", Link: ascSearchURL(repoLink, q, map[string]string{"sort": ascSearchSortPath}), Active: q.sort == ascSearchSortPath},
	}
}
