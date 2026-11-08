// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package ascsearch

import (
	"math"
	"testing"

	ascv1 "forgejo.org/modules/ascproto/ascv1"
	api "forgejo.org/modules/structs"

	"github.com/stretchr/testify/assert"
)

func TestReasonOf(t *testing.T) {
	assert.Nil(t, ReasonOf(nil, false), "an older server sends no reason")
	assert.Nil(t, ReasonOf(&ascv1.MatchReason{Kind: ascv1.MatchKind(99), Terms: []string{"x"}}, false), "a kind this hub does not know")

	r := ReasonOf(&ascv1.MatchReason{
		Kind:  ascv1.MatchKind_MATCH_KIND_TEXT,
		Score: 3,
		Terms: []string{"retry", "", "retry", "back\x1boff"},
		Spans: []*ascv1.MatchSpan{
			{Line: 3, StartCol: 1, EndCol: 6, Term: "retry"},
			{Line: 0, StartCol: 1, EndCol: 2},
			{Line: maxLine + 1, StartCol: 1, EndCol: 2},
			{Line: 4, StartCol: 5, EndCol: 2},
		},
	}, false)
	assert.Equal(t, &api.AscMatchReason{
		Kind: "text", Score: 1, Terms: []string{"retry", "backoff"},
		Spans: []api.AscMatchSpan{{Line: 3, StartCol: 1, EndCol: 6, Term: "retry"}},
	}, r, "the score is clamped, terms cleaned and deduplicated, bad spans dropped")

	r = ReasonOf(&ascv1.MatchReason{Kind: ascv1.MatchKind_MATCH_KIND_MEANING, Score: math.NaN(), Spans: []*ascv1.MatchSpan{{Line: 1, StartCol: 1, EndCol: 4}}}, true)
	assert.Equal(t, &api.AscMatchReason{Kind: "meaning"}, r, "a redacted result keeps no spans; a NaN score is 0")

	var terms []string
	for range maxReasonTerms + 4 {
		terms = append(terms, string(rune('a'+len(terms))))
	}
	assert.Len(t, ReasonOf(&ascv1.MatchReason{Kind: ascv1.MatchKind_MATCH_KIND_SYMBOL, Terms: terms}, false).Terms, maxReasonTerms)
}

func TestWhyOf(t *testing.T) {
	assert.Equal(t, Why{}, WhyOf(nil))
	assert.Equal(t, Why{}, WhyOf(&api.AscMatchReason{Kind: "text"}), "a text match with no term says nothing")
	assert.Equal(t, Why{Kind: "meaning"}, WhyOf(&api.AscMatchReason{Kind: "meaning"}), "a meaning match is said even with no term")
	assert.Equal(t, Why{Kind: "symbol", Terms: []string{"Check"}}, WhyOf(&api.AscMatchReason{Kind: "symbol", Terms: []string{"Check"}}))

	w := WhyOf(&api.AscMatchReason{
		Kind:  "text",
		Terms: []string{"retry", "backoff", "jitter", "cap", "limit"},
		Spans: []api.AscMatchSpan{{Line: 3}, {Line: 3}, {Line: 9}},
	})
	assert.Equal(t, Why{Kind: "text", Terms: []string{"retry", "backoff", "jitter"}, More: 2, Lines: 2}, w, "the first terms, the rest counted, distinct lines")
}
