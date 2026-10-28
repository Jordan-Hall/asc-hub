// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package web

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"forgejo.org/models/unittest"
	"forgejo.org/services/contexttest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAscDocsHomeAndSearch(t *testing.T) {
	unittest.PrepareTestEnv(t)

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
	unittest.PrepareTestEnv(t)

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
