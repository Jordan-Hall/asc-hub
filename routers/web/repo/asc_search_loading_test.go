// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	"strings"
	"testing"

	"forgejo.org/models/unittest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSearchLoadingState(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	page := func(set func(d map[string]any)) string {
		return renderPart(t, "repo/asc_search", func(d map[string]any) {
			d["Title"] = "search"
			d["AscSearchMode"] = "lexical"
			d["AscSearchScope"] = "version"
			set(d)
		})
	}
	for name, html := range map[string]string{
		"first visit": page(func(map[string]any) {}),
		"results": page(func(d map[string]any) {
			d["AscSearchDone"] = true
			d["AscSearchText"] = "parse"
			d["AscSearchTargets"] = 1
		}),
		"error": page(func(d map[string]any) { d["AscSearchError"] = 503 }),
	} {
		assert.Contains(t, html, `data-loading-skel data-testid="search-loading" hidden`, name)
		assert.Contains(t, html, "asc_rp.s_loading", name)
		assert.Equal(t, 3, strings.Count(html, "rp-searchload-card"), name)
		assert.Contains(t, html, `role="search" data-loading-form>`, name)
		skel := strings.Index(html, `data-testid="search-loading"`)
		body := strings.Index(html, "data-loading-body data-loading-links")
		assert.True(t, skel > 0 && body > skel, "%s: the skeleton sits before the body it replaces", name)
	}
	first := page(func(map[string]any) {})
	body := first[strings.Index(first, "data-loading-body"):]
	assert.Contains(t, body, `data-testid="asc-search-first-visit"`, "the example queries are replaced while a search loads")
}
