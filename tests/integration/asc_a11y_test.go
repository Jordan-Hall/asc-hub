// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package integration

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"forgejo.org/tests"

	"github.com/stretchr/testify/assert"
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
