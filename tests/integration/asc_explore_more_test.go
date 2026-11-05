// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package integration

import (
	"net/http"
	"strings"
	"testing"

	"forgejo.org/modules/setting"
	"forgejo.org/modules/test"
	"forgejo.org/tests"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAscExploreMore checks the phone list of Explore (board Mobile-Explore): pull to refresh, and the hidden Load
// more box with the URL of the next page, which asc-explore-more.js shows on a phone in place of the pager.
func TestAscExploreMore(t *testing.T) {
	defer tests.PrepareTestEnv(t)()
	defer test.MockVariableValue(&setting.UI.ExplorePagingNum, 2)()

	for _, c := range []struct{ path, list string }{
		{"/explore/repos", ".ds-repolist"},
		{"/explore/users", ".ds-userlist"},
	} {
		t.Run(c.path, func(t *testing.T) {
			page := NewHTMLParser(t, MakeRequest(t, NewRequest(t, "GET", c.path+"?sort=alphabetically"), http.StatusOK).Body)
			page.AssertElement(t, "[data-asc-ptr][aria-hidden=true]", true)
			page.AssertElement(t, ".ds-explore-body[data-asc-pull-refresh]", true)
			assert.Equal(t, 1, page.Find(c.list).Length())

			box := page.Find("[data-asc-more][hidden]")
			require.Equal(t, 1, box.Length())
			assert.Equal(t, c.list, box.AttrOr("data-asc-more", ""))
			next := box.AttrOr("data-asc-more-next", "")
			assert.True(t, strings.HasPrefix(next, c.path+"?page=2&"), next)
			assert.Contains(t, next, "sort=alphabetically")
			assert.Equal(t, "Load more", strings.TrimSpace(box.Find("button[data-asc-more-btn]").Text()))
			status := box.Find("[role=status][data-asc-more-status]")
			assert.Equal(t, "%d more loaded", status.AttrOr("data-template", ""))
			assert.Equal(t, "More could not load. Try again.", status.AttrOr("data-failed", ""))

			// the next page has rows for the list and its own box
			second := NewHTMLParser(t, MakeRequest(t, NewRequest(t, "GET", next), http.StatusOK).Body)
			assert.Positive(t, second.Find(c.list).Children().Length())
		})
	}

	// the last page has no box
	page := NewHTMLParser(t, MakeRequest(t, NewRequest(t, "GET", "/explore/repos?q=repo1&page=99"), http.StatusOK).Body)
	page.AssertElement(t, "[data-asc-more]", false)
}
