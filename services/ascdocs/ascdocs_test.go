// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package ascdocs_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"forgejo.org/services/ascdocs"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTheShippedPagesAreTheDocsOfTheSource(t *testing.T) {
	require.NoError(t, ascdocs.Err())
	for _, p := range ascdocs.Pages() {
		shipped, err := os.ReadFile(filepath.Join("content", p.File))
		require.NoError(t, err)
		source, err := os.ReadFile(filepath.Join("..", "..", "docs", "asc", p.File))
		require.NoError(t, err)
		assert.Equal(t, string(source), string(shipped), "%s drifted from docs/asc: run make docs-sync", p.File)
	}
}

func TestPagesChangelogAndPopularSearchesPointAtRealContent(t *testing.T) {
	pages := ascdocs.Pages()
	require.NotEmpty(t, pages)
	for _, p := range pages {
		_, body, ok := ascdocs.Get(p.Slug)
		require.True(t, ok, p.Slug)
		assert.NotEmpty(t, strings.TrimSpace(body), p.Slug)
	}
	for _, e := range ascdocs.Changelog() {
		_, _, ok := ascdocs.Get(e.Slug)
		assert.True(t, ok, "the changelog entry %q names a page", e.Title)
	}
	// newest first
	log := ascdocs.Changelog()
	for i := 1; i < len(log); i++ {
		assert.GreaterOrEqual(t, log[i-1].Date, log[i].Date)
	}
	for _, q := range ascdocs.Popular() {
		assert.NotEmpty(t, ascdocs.Search(q, 5), "the popular search %q finds something", q)
	}
}

func TestSearch(t *testing.T) {
	hits := ascdocs.Search("merge queue", 5)
	require.NotEmpty(t, hits)
	assert.NotEmpty(t, hits[0].Snippet)
	assert.Empty(t, ascdocs.Search("zzzzqqqq nothing like this", 5))
	assert.Empty(t, ascdocs.Search("   ", 5))
	assert.Empty(t, ascdocs.Search("a b c d e f g h i", 5), "too many words")
	assert.LessOrEqual(t, len(ascdocs.Search("the", 2)), 2)
	// a title match comes before a body match
	byTitle := ascdocs.Search("architecture", 3)
	require.NotEmpty(t, byTitle)
	assert.Equal(t, "architecture", byTitle[0].Page.Slug)
}

func TestLinksBetweenPagesStayInTheDocumentation(t *testing.T) {
	_, body, ok := ascdocs.Get("overview")
	require.True(t, ok)
	assert.NotContains(t, body, "](STACK-OPERATIONS.md")
	assert.NotContains(t, body, "](OPERATIONS.md")
}
