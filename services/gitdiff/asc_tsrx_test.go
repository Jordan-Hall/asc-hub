// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package gitdiff

import (
	"testing"

	"forgejo.org/modules/translation"

	"github.com/stretchr/testify/assert"
)

// A diff of a .tsrx file is highlighted with the TSRX lexer, for a whole line and for the changed words of a line.
func TestDiffOfTSRXIsHighlighted(t *testing.T) {
	line := DiffInlineWithHighlightCode("src/App.tsrx", "", "  @if (ok) {", &translation.MockLocale{})
	assert.Contains(t, string(line.Content), `>@if</span>`)

	diffs := NewHighlightCodeDiff().diffWithHighlight("src/App.tsrx", "", "    <p class=\"x\">{y}</p>\n", "    <span class=\"x\">{y}</span>\n")
	added := diffToHTML(nil, diffs, DiffLineAdd)
	assert.Contains(t, added, `<span class="added-code">`)
	assert.Contains(t, added, `>span</span>`, "the tag name is a token of its own")
}
