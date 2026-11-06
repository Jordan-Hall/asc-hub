// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package integration

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
)

// A path that is not in the version is a 404; the asc state that says where it is belongs to asc repositories, so a
// git repository keeps the plain page with its Go back link.
func TestAscNotFoundAtVersionGitRepository(t *testing.T) {
	onApplicationRun(t, func(t *testing.T, _ *url.URL) {
		session := loginUser(t, "user2")
		req := NewRequest(t, "GET", "/user2/repo1/src/branch/master/retry.rs")
		req.Header.Set("Accept", "text/html")
		resp := session.MakeRequest(t, req, http.StatusNotFound)
		doc := NewHTMLParser(t, resp.Body)
		assert.Zero(t, doc.Find(`[data-testid="asc-not-found"]`).Length())
		assert.Equal(t, "/user2/repo1/src/branch/master", doc.Find(".st-actions a.ds-btn-primary").AttrOr("href", ""))
	})
}
