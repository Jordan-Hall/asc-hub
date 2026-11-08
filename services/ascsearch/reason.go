// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package ascsearch

import (
	"math"

	ascv1 "forgejo.org/modules/ascproto/ascv1"
	api "forgejo.org/modules/structs"
)

// MatchReason is why a result matched.
type MatchReason = api.AscMatchReason

const (
	maxReasonTerms = 16
	maxReasonSpans = 32
	// maxWhyTerms is how many terms the sentence under a result names; the rest are counted.
	maxWhyTerms = 3
)

// ReasonOf checks and converts the reason of a result (docs/SPEC.md 23.4.1). It is nil when the server sent
// none or a kind this hub does not know. The terms are cleaned; a redacted result keeps no spans, whatever
// the message carried, so nothing places its text.
func ReasonOf(r *ascv1.MatchReason, redacted bool) *MatchReason {
	var kind string
	switch r.GetKind() {
	case ascv1.MatchKind_MATCH_KIND_TEXT:
		kind = "text"
	case ascv1.MatchKind_MATCH_KIND_SYMBOL:
		kind = "symbol"
	case ascv1.MatchKind_MATCH_KIND_MEANING:
		kind = "meaning"
	default:
		return nil
	}
	score := r.GetScore()
	if math.IsNaN(score) || score < 0 {
		score = 0
	}
	out := &MatchReason{Kind: kind, Score: min(score, 1)}
	seen := map[string]bool{}
	for _, t := range r.GetTerms() {
		if len(out.Terms) >= maxReasonTerms {
			break
		}
		if t = Label(t); t != "" && !seen[t] {
			seen[t] = true
			out.Terms = append(out.Terms, t)
		}
	}
	if redacted {
		return out
	}
	for _, s := range r.GetSpans() {
		if len(out.Spans) >= maxReasonSpans {
			break
		}
		if s.GetLine() < 1 || s.GetLine() > maxLine || s.GetEndCol() < s.GetStartCol() {
			continue
		}
		out.Spans = append(out.Spans, api.AscMatchSpan{Line: s.GetLine(), StartCol: s.GetStartCol(), EndCol: s.GetEndCol(), Term: Label(s.GetTerm())})
	}
	return out
}

// Why is the sentence under a result that says why it matched. Kind is empty when nothing is said.
type Why struct {
	// Kind is text, symbol or meaning.
	Kind string
	// Terms are the first matched terms; More counts the others.
	Terms []string
	More  int
	// Lines is how many lines hold a match; 0 when the server placed none (a hidden file).
	Lines int
}

// WhyOf builds the sentence of a reason. A text or symbol match that names no term says nothing the result
// does not already show, so it is left out; a match by meaning is said even without a term.
func WhyOf(r *MatchReason) Why {
	if r == nil || (r.Kind != "meaning" && len(r.Terms) == 0) {
		return Why{}
	}
	w := Why{Kind: r.Kind}
	if len(r.Terms) > maxWhyTerms {
		w.Terms, w.More = r.Terms[:maxWhyTerms], len(r.Terms)-maxWhyTerms
	} else {
		w.Terms = r.Terms
	}
	lines := map[uint32]bool{}
	for _, s := range r.Spans {
		lines[s.Line] = true
	}
	w.Lines = len(lines)
	return w
}
