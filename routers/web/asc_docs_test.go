// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package web

import (
	"html/template"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"forgejo.org/services/contexttest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAscDocsHomeAndSearch(t *testing.T) {

	ctx, resp := contexttest.MockContext(t, "GET /-/docs")
	AscDocs(ctx)
	require.Equal(t, http.StatusOK, resp.Code)
	assert.NotEmpty(t, ctx.Data["DocsPages"])
	assert.NotEmpty(t, ctx.Data["DocsPopular"])
	assert.NotEmpty(t, ctx.Data["DocsNew"])
	assert.Nil(t, ctx.Data["DocsSearched"])

	ctx, resp = contexttest.MockContext(t, "GET /-/docs?q=merge+queue")
	ctx.Req.Form = url.Values{"q": {"merge queue"}}
	AscDocs(ctx)
	require.Equal(t, http.StatusOK, resp.Code)
	assert.Equal(t, true, ctx.Data["DocsSearched"])
	assert.NotEmpty(t, ctx.Data["DocsHits"])

	// a very long query is cut, never refused or reflected whole
	ctx, _ = contexttest.MockContext(t, "GET /-/docs")
	ctx.Req.Form = url.Values{"q": {strings.Repeat("a", 5000)}}
	AscDocs(ctx)
	assert.Len(t, ctx.Data["DocsQuery"], 100)
}

func TestAscDocsPage(t *testing.T) {

	ctx, resp := contexttest.MockContext(t, "GET /-/docs/overview")
	ctx.SetParams(":slug", "overview")
	AscDocsPage(ctx)
	require.Equal(t, http.StatusOK, resp.Code)
	assert.NotEmpty(t, ctx.Data["DocsHTML"])

	// an unknown page, and a name that tries to leave the documentation, are plain not found
	for _, slug := range []string{"no-such-page", "../../app.ini", "overview.md"} {
		ctx, resp = contexttest.MockContext(t, "GET /-/docs/x")
		ctx.SetParams(":slug", slug)
		AscDocsPage(ctx)
		assert.Equal(t, http.StatusNotFound, resp.Code, slug)
	}
}

func TestAscServiceWorkerIsScriptForTheWholeHub(t *testing.T) {
	ctx, resp := contexttest.MockContext(t, "GET /-/asc-sw.js")
	AscServiceWorker(ctx)
	require.Equal(t, http.StatusOK, resp.Code)
	assert.Contains(t, resp.Header().Get("Content-Type"), "javascript")
	assert.Equal(t, "/", resp.Header().Get("Service-Worker-Allowed"))
	assert.Equal(t, "nosniff", resp.Header().Get("X-Content-Type-Options"))
	body := resp.Body.String()
	assert.Contains(t, body, `addEventListener("push"`)
	assert.Contains(t, body, `addEventListener("fetch"`)
	assert.Contains(t, body, `-/offline`)
	assert.Contains(t, body, `startsWith("//")`, "a click opens a path of this site only")
}

func TestAscDocsPageTablesScrollAndAreReachable(t *testing.T) {
	ctx, resp := contexttest.MockContext(t, "GET /-/docs/stacks-and-operations")
	ctx.SetParams(":slug", "stacks-and-operations")
	AscDocsPage(ctx)
	require.Equal(t, http.StatusOK, resp.Code)
	html := string(ctx.Data["DocsHTML"].(template.HTML))
	assert.Contains(t, html, `<div class="site-table" role="region" tabindex="0"`)
	assert.Equal(t, strings.Count(html, "<table"), strings.Count(html, "</table></div>"))
}
