// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package integration

import (
	"net/http"
	"net/url"
	"testing"

	"forgejo.org/tests"

	"github.com/PuerkitoBio/goquery"

	"github.com/stretchr/testify/assert"
)

// `label:` and `author:` in the search box of the issue list are the filters of the label and author menus.
func TestIssueSearchLabelAndAuthorFilters(t *testing.T) {
	defer tests.PrepareTestEnv(t)()

	titles := func(q string) []string {
		req := NewRequest(t, "GET", "/user2/repo1/issues?state=all&q="+url.QueryEscape(q))
		resp := MakeRequest(t, req, http.StatusOK)
		doc := NewHTMLParser(t, resp.Body)
		var out []string
		doc.Find("#issue-list .wk-issue").Each(func(_ int, s *goquery.Selection) {
			out = append(out, s.Find("a.issue-title, .issue-title").First().Text())
		})
		return out
	}

	all := titles("")
	assert.NotEmpty(t, all, "the fixtures have issues in repository 1")

	withLabel := titles("label:label1")
	assert.NotEmpty(t, withLabel)
	assert.Less(t, len(withLabel), len(all), "a label narrows the list")

	assert.Empty(t, titles("label:there-is-no-such-label"), "an unknown label matches nothing, not everything")
	assert.Empty(t, titles("author:there-is-no-such-person"))

	byUser1 := titles("author:user1")
	assert.NotEmpty(t, byUser1)
	assert.LessOrEqual(t, len(byUser1), len(all))

	// both filters together are the intersection
	both := titles("label:label1 author:user1")
	assert.LessOrEqual(t, len(both), len(withLabel))
	assert.LessOrEqual(t, len(both), len(byUser1))
}
