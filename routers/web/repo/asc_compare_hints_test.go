// Copyright 2026 The ASC Hub Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package repo

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSuggestRef(t *testing.T) {
	refs := []string{"main", "release/2.18", "retry-with-backoff", "refund-idempotency"}
	assert.Equal(t, "retry-with-backoff", suggestRef("retry-with-backof", refs))
	assert.Equal(t, "retry-with-backoff", suggestRef("Retry-With-Backoff", refs), "case is not a difference")
	assert.Equal(t, "main", suggestRef("mian", refs))
	assert.Empty(t, suggestRef("main", refs), "a known name is not corrected")
	assert.Empty(t, suggestRef("something-else-entirely", refs))
	assert.Empty(t, suggestRef("", refs))
	assert.Empty(t, suggestRef("x", nil))
}

func TestCompareHintsSkipKnownAndCurrent(t *testing.T) {
	groups := []ascRefGroup{
		{Label: "asc_rp.cmp_shared", Names: []string{"main"}},
		{Label: "asc_rp.cmp_stacks", Names: []string{"retry-with-backoff"}},
		{Label: "asc_rp.cmp_current", Names: []string{"retry-with-backof"}},
	}
	got := compareHints("main", "retry-with-backof", groups)
	if assert.Len(t, got, 1) {
		assert.Equal(t, ascDidYouMean{Side: "b", Given: "retry-with-backof", Hint: "retry-with-backoff"}, got[0])
	}
	assert.Empty(t, compareHints("main", "retry-with-backoff", groups))
}

func TestParseNumstat(t *testing.T) {
	got := parseNumstat("12\t3\tsrc/a.rs\n-\t-\tlogo.png\nbad line\n0\t5\tdir/b c.txt\nx\ty\tz\n")
	assert.Equal(t, map[string]numstat{
		"src/a.rs":    {Add: 12, Del: 3},
		"logo.png":    {Binary: true},
		"dir/b c.txt": {Add: 0, Del: 5},
	}, got)
}

func TestEditDistance(t *testing.T) {
	assert.Equal(t, 0, editDistance("abc", "abc"))
	assert.Equal(t, 1, editDistance("abc", "abd"))
	assert.Equal(t, 3, editDistance("", "abc"))
	assert.Equal(t, 3, editDistance("kitten", "sitting"))
}
