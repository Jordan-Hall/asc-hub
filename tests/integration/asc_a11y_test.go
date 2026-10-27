// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package integration

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"forgejo.org/tests"

	"github.com/PuerkitoBio/goquery"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Every page has a skip link as its first link, and its target exists exactly once, after the navigation bar.
func TestAscSkipLinkOnEveryPage(t *testing.T) {
	defer tests.PrepareTestEnv(t)()
	user := loginUser(t, "user2")
	for _, path := range []string{"/", "/user/login", "/explore/repos", "/user2/repo1", "/user/settings"} {
		var resp *httptest.ResponseRecorder
		if path == "/user/login" { // a signed-in person is sent away from it
			resp = MakeRequest(t, NewRequest(t, "GET", path), http.StatusOK)
		} else {
			resp = user.MakeRequest(t, NewRequest(t, "GET", path), http.StatusOK)
		}
		doc := NewHTMLParser(t, resp.Body)
		skip := doc.doc.Find("a.skip-link")
		assert.Equal(t, 1, skip.Length(), path)
		href, _ := skip.Attr("href")
		assert.Equal(t, "#main-content", href, path)
		assert.NotEmpty(t, skip.Text(), path+": the link has a text")
		assert.Equal(t, 1, doc.doc.Find("#main-content").Length(), path+": the target exists once")
		first := doc.doc.Find("body a, body button, body input").First()
		assert.True(t, first.HasClass("skip-link"), path+": the skip link is the first thing to tab to")
	}
}

// Controls that axe found without a name, in a role that does not allow what they carry, or nested in another control.
func TestAscControlsAreNamedAndNotNested(t *testing.T) {
	defer tests.PrepareTestEnv(t)()
	user := loginUser(t, "user2")

	// A summary is a button: it holds no link. The list of watchers is the first entry of its menu.
	doc := NewHTMLParser(t, user.MakeRequest(t, NewRequest(t, "GET", "/user2/repo1"), http.StatusOK).Body)
	assert.Zero(t, doc.doc.Find("#watch-button summary a").Length(), "no link inside the summary")
	assert.Equal(t, 1, doc.doc.Find("#watch-button .content a[href$='/watchers']").Length())
	assert.Equal(t, 1, doc.doc.Find("#watch-button summary .label").Length(), "the count stays visible")

	// A fork button that cannot be used is disabled for assistive technology too, not only dimmed.
	anon := NewHTMLParser(t, MakeRequest(t, NewRequest(t, "GET", "/user2/repo1"), http.StatusOK).Body)
	disabled := anon.doc.Find(".ui.labeled.button.disabled a.ui.button[aria-disabled='true']")
	assert.GreaterOrEqual(t, disabled.Length(), 1, "the fork button of a guest")

	// The fold arrow of a file of a diff has a name.
	diff := NewHTMLParser(t, user.MakeRequest(t, NewRequest(t, "GET", "/user2/repo1/commit/65f1bf27bc3bf70f64657658635e66094edbcb4d"), http.StatusOK).Body)
	fold := diff.doc.Find("button.fold-file")
	require.Greater(t, fold.Length(), 0)
	fold.Each(func(_ int, b *goquery.Selection) {
		label, _ := b.Attr("aria-label")
		assert.NotEmpty(t, label)
		typ, _ := b.Attr("type")
		assert.Equal(t, "button", typ)
	})
}
