// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	gotemplate "html/template"
	"strings"
	"testing"

	access_model "forgejo.org/models/perm/access"
	unit_model "forgejo.org/models/unit"
	"forgejo.org/models/unittest"
	"forgejo.org/modules/charset"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBlamePhoneSheet(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	html := renderPart(t, "repo/blame", func(d map[string]any) {
		d["FileName"] = "a.go"
		d["TreePath"] = "a.go"
		d["CommitID"] = "abc"
		d["CommitCnt"] = 2
		d["Permission"] = &access_model.Permission{}
		d["EscapeStatus"] = &charset.EscapeStatus{}
		d["UnitTypeIssues"] = unit_model.TypeIssues
		d["BlameRows"] = []*blameRow{
			{RowNumber: 1, Avatar: gotemplate.HTML(`<img alt="">`), AuthorName: "Ada <x>", CommitMessage: "Parse", CommitURL: "/c/abc", PartSha: "abc", Code: "a", EscapeStatus: &charset.EscapeStatus{}},
			{RowNumber: 2, Code: "b", EscapeStatus: &charset.EscapeStatus{}},
		}
	})
	assert.Equal(t, 1, strings.Count(html, "data-blame-open"), "only the first line of a change is a button")
	assert.Contains(t, html, `aria-label="asc_rp.blame_who`)
	assert.NotContains(t, html, "Ada <x>", "the author is escaped")
	assert.Contains(t, html, `data-testid="blame-sheet"`)
	assert.Contains(t, html, `data-t-line="asc_rp.blame_line`)
	assert.Contains(t, html, `data-t-lines="asc_rp.blame_lines`)
	for _, part := range []string{"data-blame-sheet-change hidden", "data-blame-sheet-line hidden", "data-blame-share hidden", "data-blame-copy", `<form method="dialog">`} {
		assert.Contains(t, html, part)
	}
	sheet := strings.Index(html, `data-testid="blame-sheet"`)
	menu := strings.Index(html, "code-line-menu")
	assert.True(t, menu > 0 && sheet > menu, "the sheet reads the line menu the code view keeps up to date")
}
