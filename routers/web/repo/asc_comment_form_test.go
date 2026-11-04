// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	"testing"

	issues_model "forgejo.org/models/issues"
	"forgejo.org/models/unittest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTheLineCommentFormIsTheComposerOfTheBoard(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	out := renderPart(t, "repo/diff/new_comment", func(d map[string]any) {
		d["SignedUserID"] = int64(2)
		d["PageIsPullFiles"] = true
	})
	for _, want := range []string{
		`data-asc-ph-new="repo.diff.comment.asc_on_line"`, `data-asc-ph-old-lines="repo.diff.comment.asc_on_old_lines"`,
		`data-asc-suggest-label="repo.diff.comment.asc_suggest"`, `<text-expander keys=": @ /"`,
		`placeholder="repo.diff.comment.asc_placeholder"`, "repo.diff.comment.asc_info<",
		`name="single_review" value="true"`, "repo.diff.comment.asc_comment<", "cancel-code-comment",
	} {
		assert.Contains(t, out, want)
	}
	for _, gone := range []string{"start_review", "add_single_comment", "markdown_info", "octicon-markdown", `name="pending_review"`} {
		assert.NotContains(t, out, gone)
	}

	out = renderPart(t, "repo/diff/new_comment", func(d map[string]any) {
		d["SignedUserID"] = int64(2)
		d["CurrentReview"] = &issues_model.Review{ID: 1}
	})
	assert.Contains(t, out, `name="pending_review"`, "with a review open, the comment goes into it")
	assert.Contains(t, out, "repo.diff.comment.asc_info_review")
	assert.NotContains(t, out, `name="single_review"`)

	out = renderPart(t, "repo/diff/comment_form_datahandler", func(d map[string]any) {
		d["SignedUserID"] = int64(2)
		d["CurrentReview"] = &issues_model.Review{ID: 1}
		d["root"], d["reply"], d["hidden"], d["comment"] = d, int64(5), true, &issues_model.Comment{TreePath: "src/a.go"}
	})
	for _, want := range []string{`<text-expander keys=": @"`, `placeholder="repo.diff.comment.placeholder"`, "repo.diff.comment.asc_info<", `name="reply" value="5"`} {
		assert.Contains(t, out, want)
	}
	assert.NotContains(t, out, "data-asc-ph-new", "a reply is not on lines of its own")
}
