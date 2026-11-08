// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	ascv1 "forgejo.org/modules/ascproto/ascv1"
)

// ascSearchMaxWhyTerms is how many matched terms the "why it matched" line names; the rest are counted.
const ascSearchMaxWhyTerms = 3

// ascSearchWhy is why one result matched, built from the server's MatchReason (docs/SPEC.md 23.4.1). Kind is
// empty when the answer carried no reason (an older server) or one this page does not know; nothing is drawn.
type ascSearchWhy struct {
	// Kind is text (the text of the file matched), symbol (a symbol name matched) or meaning (the chunk is
	// close to the question).
	Kind string
	// Terms are the query terms that matched, cleaned, at most ascSearchMaxWhyTerms; More counts the others.
	Terms []string
	More  int
	// Lines is how many lines of the result hold a match; 0 when the server sent no spans (a hidden file).
	Lines int
}

// ascSearchWhyOf reads a reason. Terms that clean to nothing are dropped.
func ascSearchWhyOf(r *ascv1.MatchReason) ascSearchWhy {
	var w ascSearchWhy
	switch r.GetKind() {
	case ascv1.MatchKind_MATCH_KIND_TEXT:
		w.Kind = "text"
	case ascv1.MatchKind_MATCH_KIND_SYMBOL:
		w.Kind = "symbol"
	case ascv1.MatchKind_MATCH_KIND_MEANING:
		w.Kind = "meaning"
	default:
		return w
	}
	seen := map[string]bool{}
	for _, t := range r.GetTerms() {
		t = ascSymbolsText([]byte(t), ascSymbolsMaxShortTxt)
		if t == "" || seen[t] {
			continue
		}
		seen[t] = true
		if len(w.Terms) < ascSearchMaxWhyTerms {
			w.Terms = append(w.Terms, t)
		} else {
			w.More++
		}
	}
	lines := map[uint32]bool{}
	for _, s := range r.GetSpans() {
		if s.GetLine() >= 1 && s.GetLine() <= ascSymbolsMaxLine {
			lines[s.GetLine()] = true
		}
	}
	w.Lines = len(lines)
	if w.Kind != "meaning" && len(w.Terms) == 0 {
		w.Kind = "" // a text or symbol match with no term to name says nothing the result does not show
	}
	return w
}
