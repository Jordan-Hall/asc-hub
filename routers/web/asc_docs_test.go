// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package web

import (
	"html/template"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"forgejo.org/modules/templates"
	"forgejo.org/services/ascdocs"
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
	assert.Contains(t, body, `<script[^>]*src=`, "the scripts of the offline page are kept, to say when an edit is kept")
	assert.Contains(t, body, `"asc-offline-v2"`, "a new cache name drops the cache of the worker before")
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

func TestAscPaletteNeedsAQueryOfTheRightLength(t *testing.T) {
	for _, q := range []string{"", "a", strings.Repeat("x", ascPaletteMaxQuery+1)} {
		ctx, resp := contexttest.MockContext(t, "GET /-/asc/palette")
		ctx.Req.Form = map[string][]string{"q": {q}}
		AscPalette(ctx)
		require.Equal(t, http.StatusOK, resp.Code)
		assert.JSONEq(t, `{"groups":[]}`, resp.Body.String(), "query %d bytes", len(q))
		assert.Equal(t, "private, no-store", resp.Header().Get("Cache-Control"))
	}
}

func TestAscDocsSearchGroupsCommandsAndGuides(t *testing.T) {
	ctx, resp := contexttest.MockContext(t, "GET /-/docs?q=submit")
	ctx.Req.Form = url.Values{"q": {"submit"}}
	AscDocs(ctx)
	require.Equal(t, http.StatusOK, resp.Code)
	cmds, ok := ctx.Data["DocsCommands"].([]ascdocs.Command)
	require.True(t, ok)
	require.NotEmpty(t, cmds)
	assert.Equal(t, "asc submit", cmds[0].Name)
	assert.NotEmpty(t, ctx.Data["DocsHits"])
	assert.Empty(t, ctx.Data["DocsSuggest"])

	// the reference page carries the heading the command links to
	ctx, resp = contexttest.MockContext(t, "GET /-/docs/cli")
	ctx.SetParams(":slug", "cli")
	AscDocsPage(ctx)
	require.Equal(t, http.StatusOK, resp.Code)
	assert.Contains(t, string(ctx.Data["DocsHTML"].(template.HTML)), `id="user-content-`+cmds[0].Anchor+`"`)
}

func TestAscDocsSearchWithoutResultsSuggestsAWord(t *testing.T) {
	ctx, resp := contexttest.MockContext(t, "GET /-/docs?q=rebse")
	ctx.Req.Form = url.Values{"q": {"rebse"}}
	AscDocs(ctx)
	require.Equal(t, http.StatusOK, resp.Code)
	assert.Empty(t, ctx.Data["DocsCommands"])
	assert.Empty(t, ctx.Data["DocsHits"])
	assert.Equal(t, "rebase", ctx.Data["DocsSuggest"])
}

func TestAscDocsSearchFragmentIsTheResultsAlone(t *testing.T) {
	ctx, resp := contexttest.MockContext(t, "GET /-/docs?q=submit&fragment=results")
	ctx.Req.Form = url.Values{"q": {"submit"}, "fragment": {"results"}}
	ctx.Render = templates.HTMLRenderer()
	AscDocs(ctx)
	require.Equal(t, http.StatusOK, resp.Code)
	assert.Equal(t, "private, no-store", resp.Header().Get("Cache-Control"))
	body := resp.Body.String()
	assert.Contains(t, body, "data-asc-docs-results")
	assert.Contains(t, body, "/-/docs/cli#6-stacks-of-changes")
	assert.NotContains(t, body, "<html")
	assert.NotContains(t, body, "data-asc-docs-live")
}
