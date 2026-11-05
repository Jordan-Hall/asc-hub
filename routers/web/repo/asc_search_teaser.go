// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	gocontext "context"
	"time"

	ascv1 "forgejo.org/modules/ascproto/ascv1"
)

// "The other two kinds of search" (board Search): under the results of one kind, what the other two find
// for the same words. Text and Symbol search are index lookups, so they are run and counted; Meaning search
// costs a model call, so it is offered as a link without a count. A failed lookup only hides its line.

const (
	ascSearchTeaserTimeout = 3 * time.Second
	ascSearchTeaserPaths   = 3
)

// ascSearchTeaser is one other kind of search.
type ascSearchTeaser struct {
	Mode  string // lexical, symbol or semantic
	Link  string
	Count int // -1 when not counted
	More  bool
	Paths []ascSearchAlso
}

// ascSearchTeasers runs the other kinds that are cheap and returns the lines to show, in board order.
func ascSearchTeasers(ctx gocontext.Context, api ascv1.CodeIntelClient, scope *ascv1.Scope, repoLink string, q ascSearchQuery) []ascSearchTeaser {
	var out []ascSearchTeaser
	for _, mode := range []string{ascSearchModeLexical, ascSearchModeSymbol, ascSearchModeSemantic} {
		if mode == q.mode {
			continue
		}
		o := q
		o.mode, o.match, o.caseSensitive = mode, ascSearchMatchLiteral, false
		t := ascSearchTeaser{Mode: mode, Count: -1, Link: ascSearchURL(repoLink, o, map[string]string{"sort": ""})}
		switch mode {
		case ascSearchModeSemantic:
			if len(q.text) > ascSearchMaxSemanticBytes {
				continue
			}
			out = append(out, t)
			continue
		case ascSearchModeSymbol:
			if len(q.text) > ascSearchMaxSymbolBytes {
				continue
			}
		}
		callCtx, cancel := gocontext.WithTimeout(ctx, ascSearchTeaserTimeout)
		var res ascSearchResult
		var err error
		if mode == ascSearchModeLexical {
			res, err = ascSearchLexical(callCtx, api, scope, o)
		} else {
			res, err = ascSearchSymbols(callCtx, api, scope, o)
		}
		cancel()
		if err != nil {
			continue
		}
		t.Count, t.More = len(res.files)+len(res.symbols), res.truncated
		if t.Count == 0 {
			continue
		}
		for _, f := range res.files {
			if len(t.Paths) < ascSearchTeaserPaths {
				t.Paths = append(t.Paths, ascSearchAlso{Path: f.Path, Link: f.Link})
			}
		}
		for _, s := range res.symbols {
			if len(t.Paths) < ascSearchTeaserPaths {
				t.Paths = append(t.Paths, ascSearchAlso{Path: s.Name, Link: s.Link})
			}
		}
		out = append(out, t)
	}
	return out
}
