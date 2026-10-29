// Copyright 2026 The ASC Hub Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package repo

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestChangeIntentOf(t *testing.T) {
	cases := []struct{ name, in, want string }{
		{"title only", "Fix refund timeout", ""},
		{"title and reason", "Fix refund timeout\n\nThe refund call waited forever when the ledger was slow.", "The refund call waited forever when the ledger was slow."},
		{"trailers are dropped", "Fix\n\nWhy it matters.\n\nSigned-off-by: A <a@x.org>\nCo-authored-by: B <b@x.org>\n", "Why it matters."},
		{"a colon in the prose is kept", "Fix\n\nNote: this changes the default.\nIt also logs.", "Note: this changes the default.\nIt also logs."},
		{"only trailers", "Fix\n\nSigned-off-by: A <a@x.org>", ""},
		{"crlf", "Fix\r\n\r\nBecause.\r\n", "Because."},
		{"empty", "", ""},
	}
	for _, c := range cases {
		assert.Equal(t, c.want, changeIntentOf(c.in), c.name)
	}
}

func TestIsTrailerLine(t *testing.T) {
	assert.True(t, isTrailerLine("Signed-off-by: A <a@x.org>"))
	assert.True(t, isTrailerLine("Change-Id: abc"))
	assert.False(t, isTrailerLine("Note this: x"))
	assert.False(t, isTrailerLine("Key:"))
	assert.False(t, isTrailerLine("no colon"))
	assert.False(t, isTrailerLine(": value"))
}
