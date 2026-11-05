// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package integration

import (
	"net/http"
	"testing"

	"forgejo.org/tests"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAscExploreNoMatch(t *testing.T) {
	defer tests.PrepareTestEnv(t)()

	doc := NewHTMLParser(t, MakeRequest(t, NewRequest(t, "GET", "/explore/repos?q=ledgr"), http.StatusOK).Body)
	empty := doc.doc.Find("[data-asc-explore-nomatch]")
	require.Equal(t, 1, empty.Length())
	assert.Equal(t, "Nothing matches “ledgr”", empty.Find("h2").Text())
	assert.Contains(t, empty.Find("p").First().Text(), "1 filter is active.")
	assert.Equal(t, 1, doc.doc.Find(".ds-active-filters").Length(), "the chips show once, in the empty state")
	assert.Equal(t, 1, empty.Find(".ds-active-filters a.ds-chip-btn").Length())
	assert.Zero(t, empty.Find(".ds-active-filters .ds-link-quiet").Length(), "the state has its own Clear filters button")
	assert.Equal(t, "?sort=recentupdate", empty.Find(".ds-empty-acts a.ds-btn-primary").AttrOr("href", ""), "Clear filters keeps the sort")

	doc = NewHTMLParser(t, MakeRequest(t, NewRequest(t, "GET", "/explore/repos?q=ledgr&language=Go"), http.StatusOK).Body)
	empty = doc.doc.Find("[data-asc-explore-nomatch]")
	assert.Contains(t, empty.Find("p").First().Text(), "2 filters are active.")
	assert.Equal(t, 2, empty.Find("a.ds-chip-btn").Length())

	// with results the chips stay above the list
	doc = NewHTMLParser(t, MakeRequest(t, NewRequest(t, "GET", "/explore/repos?q=repo1"), http.StatusOK).Body)
	assert.Zero(t, doc.doc.Find("[data-asc-explore-nomatch]").Length())
	assert.Equal(t, 1, doc.doc.Find(".ds-active-filters .ds-link-quiet").Length())
}
