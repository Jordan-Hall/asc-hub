// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package integration

import (
	"net/http"
	"strings"
	"testing"

	"forgejo.org/tests"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAscOfflineDraft checks the offline state of an edit (board Errors, Offline): the offline page has the line for a
// kept edit and Retry now, and a signed-in page has the note asc-drafts.js puts on a form held while offline.
func TestAscOfflineDraft(t *testing.T) {
	defer tests.PrepareTestEnv(t)()

	page := NewHTMLParser(t, MakeRequest(t, NewRequest(t, "GET", "/-/offline"), http.StatusOK).Body)
	assert.Equal(t, "You are offline", strings.TrimSpace(page.Find("h1#st-title").Text()))
	page.AssertElement(t, "p[data-asc-offline-text]:not([hidden])", true)
	draft := page.Find("p[data-asc-offline-draft][hidden]")
	require.Equal(t, 1, draft.Length())
	assert.Equal(t, "Your edit is saved on this device and will be sent when the connection returns.", strings.TrimSpace(draft.Text()))
	assert.Equal(t, "Retry now", strings.TrimSpace(page.Find(".st-actions a.ds-btn-primary").Text()))
	page.AssertElement(t, "template[data-asc-draft-offline]", false)

	session := loginUser(t, "user2")
	page = NewHTMLParser(t, session.MakeRequest(t, NewRequest(t, "GET", "/user2/repo1/issues/1"), http.StatusOK).Body)
	assert.Positive(t, page.Find(".combo-markdown-editor textarea[name=content]").Length())
	tmpl := page.Find("template[data-asc-draft-offline]")
	require.Equal(t, 1, tmpl.Length())
	html, err := tmpl.Html()
	require.NoError(t, err)
	assert.Contains(t, html, `role="status"`)
	assert.Contains(t, html, "Your edit is saved on this device and will be sent when the connection returns.")
	assert.Contains(t, html, "Retry now")
}
