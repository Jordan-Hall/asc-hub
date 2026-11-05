// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package explore

import (
	"cmp"
	"net/url"
	"slices"
)

// The Repository facet of the search over all repositories (board Search): the repositories of the answer
// with the number of hits in each, most first. A repository opens the same search in that repository, where
// the version, change, stack and branch scopes are.

// ascExploreRepoFacetsShown repositories are listed; the others are behind "N more".
const ascExploreRepoFacetsShown = 3

// ascExploreRepoModes maps a mode of this page to the same mode of the repository search.
var ascExploreRepoModes = map[string]string{
	ascExploreModeText:    "lexical",
	ascExploreModeSymbol:  "symbol",
	ascExploreModeMeaning: "semantic",
}

// ascExploreRepoFacets counts the hits of each group and links each repository to its own search.
func ascExploreRepoFacets(q ascExploreQuery, groups []ascExploreGroup) (shown, more []ascExploreFacet) {
	all := make([]ascExploreFacet, 0, len(groups))
	for _, g := range groups {
		n := len(g.Files) + len(g.Symbols) + len(g.Chunks)
		if n == 0 {
			continue
		}
		v := url.Values{}
		v.Set("q", q.text)
		v.Set("mode", ascExploreRepoModes[q.mode])
		if q.mode == ascExploreModeText {
			if q.match != "" {
				v.Set("match", q.match)
			}
			if q.cs {
				v.Set("case", "1")
			}
		}
		if q.lang != "" {
			v.Set("lang", q.lang)
		}
		if q.path != "" {
			v.Set("path", q.path)
		}
		all = append(all, ascExploreFacet{Name: g.Name, Count: n, URL: g.Link + "/asc/search?" + v.Encode()})
	}
	slices.SortStableFunc(all, func(a, b ascExploreFacet) int {
		return cmp.Or(cmp.Compare(b.Count, a.Count), cmp.Compare(a.Name, b.Name))
	})
	if len(all) <= ascExploreRepoFacetsShown {
		return all, nil
	}
	return all[:ascExploreRepoFacetsShown], all[ascExploreRepoFacetsShown:]
}
