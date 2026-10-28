// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package integration

import (
	"net/http"
	"testing"

	"forgejo.org/tests"

	"github.com/stretchr/testify/assert"
)

// A browser that is refused sees the status page; a client that does not ask for HTML keeps the plain answer.
func TestAscForbiddenPage(t *testing.T) {
	defer tests.PrepareTestEnv(t)()
	session := loginUser(t, "user2")

	req := NewRequest(t, "GET", "/admin")
	req.Header.Set("Accept", "text/html")
	doc := NewHTMLParser(t, session.MakeRequest(t, req, http.StatusForbidden).Body)
	assert.Equal(t, 1, doc.doc.Find(`.st-card h1#st-title`).Length())
	assert.Equal(t, 1, doc.doc.Find(`.st-code`).Length())
	assert.Equal(t, 1, doc.doc.Find(`a.ds-btn[href$="/explore/repos"]`).Length())

	req = NewRequest(t, "GET", "/admin")
	req.Header.Set("Accept", "application/json")
	resp := session.MakeRequest(t, req, http.StatusForbidden)
	assert.NotContains(t, resp.Body.String(), "st-card")
}
