// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package ascdocs

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSearchCommands(t *testing.T) {
	hits := SearchCommands("submit", 5)
	require.NotEmpty(t, hits)
	assert.Equal(t, "asc submit", hits[0].Name, "a name match comes before a summary match")
	assert.Equal(t, "Stacks of changes", hits[0].Group)
	assert.Equal(t, "6-stacks-of-changes", hits[0].Anchor)
	assert.Equal(t, "Sends the stack to the server as draft changes.", hits[0].Summary)

	// every word must match; the limit holds; a blank or very long query finds nothing
	assert.Empty(t, SearchCommands("submit zzzzqx", 5))
	assert.Len(t, SearchCommands("the", 2), 2)
	assert.Empty(t, SearchCommands("   ", 5))
	assert.Empty(t, SearchCommands("a b c d e f g h i", 5))

	auth := SearchCommands("auth", 10)
	require.NotEmpty(t, auth)
	for _, c := range auth {
		assert.NotContains(t, c.Summary, "`", "the summary has no markdown marks")
	}
	assert.Equal(t, "asc auth status", auth[0].Name)
	assert.Equal(t, "1-set-up-and-look-after-an-install", auth[0].Anchor)
}

func TestSuggest(t *testing.T) {
	assert.Equal(t, "submit", Suggest("submitt"))
	assert.Equal(t, "stack submit", Suggest("stack submitt"), "known words stay")
	assert.Empty(t, Suggest("submit"), "nothing to change")
	assert.Empty(t, Suggest("qqqqqqqq"), "nothing close enough")
	assert.Empty(t, Suggest("sbm"), "short words are left alone")
	assert.Empty(t, Suggest(""))
}

func TestEditDistance(t *testing.T) {
	assert.Equal(t, 0, editDistance("stack", "stack", 3))
	assert.Equal(t, 1, editDistance("submitt", "submit", 3))
	assert.Equal(t, 2, editDistance("restack", "rstak", 3))
	assert.Equal(t, 3, editDistance("abcdef", "uvwxyz", 3), "capped at the limit")
	assert.Equal(t, 3, editDistance("ab", "abcdefg", 3), "a length gap past the limit stops early")
}

func TestAnchorOf(t *testing.T) {
	assert.Equal(t, "6-stacks-of-changes", anchorOf("6. Stacks of changes"))
	assert.Equal(t, "2-clone--fetch", anchorOf("2. Clone & fetch"))
}
