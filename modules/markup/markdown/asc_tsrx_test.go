// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package markdown_test

import (
	"testing"

	"forgejo.org/modules/git"
	"forgejo.org/modules/markup"
	"forgejo.org/modules/markup/markdown"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A ```tsrx fence in Markdown (a README, a comment, the docs) is highlighted with the TSRX lexer: the control flow
// keyword and the tag name are tokens of their own, not one plain run of text.
func TestRenderTSRXFence(t *testing.T) {
	out, err := markdown.RenderString(&markup.RenderContext{Ctx: git.DefaultContext},
		"```tsrx\nfunction A() @{\n  @if (ok) {\n    <p class=\"x\">{y}</p>\n  }\n}\n```\n")
	require.NoError(t, err)
	html := string(out)
	assert.Contains(t, html, `<code class="chroma language-tsrx display">`)
	assert.Contains(t, html, `>@if</span>`)
	assert.Contains(t, html, `>p</span>`)
}
