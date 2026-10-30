// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	"strings"
	"testing"

	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/services/gitdiff"

	"github.com/stretchr/testify/assert"
)

func TestAscHunkContextText(t *testing.T) {
	chain := []*ascv1.HunkContextEntry{{Name: "UserList", Kind: "component"}, {Name: "@for (const user of users)", Kind: "scope"}, {Name: "@if (user.bio)", Kind: "scope"}}
	assert.Equal(t, "UserList > @for (const user of users) > @if (user.bio)", ascHunkContextText(chain))
	assert.Empty(t, ascHunkContextText(nil))
	assert.Empty(t, ascHunkContextText([]*ascv1.HunkContextEntry{{Name: "  "}}))
	long := ascHunkContextText([]*ascv1.HunkContextEntry{{Name: strings.Repeat("a", 79)}, {Name: strings.Repeat("b", 79)}, {Name: strings.Repeat("c", 79)}})
	assert.LessOrEqual(t, len(long), ascHunkContextMaxText)
}

func TestAscHunkStarts(t *testing.T) {
	f := &gitdiff.DiffFile{Sections: []*gitdiff.DiffSection{
		{Lines: []*gitdiff.DiffLine{
			{Type: gitdiff.DiffLineSection, SectionInfo: &gitdiff.DiffLineSectionInfo{RightIdx: 14}},
			{Type: gitdiff.DiffLinePlain},
		}},
		{Lines: []*gitdiff.DiffLine{
			{Type: gitdiff.DiffLineSection, SectionInfo: &gitdiff.DiffLineSectionInfo{RightIdx: 14}},
			{Type: gitdiff.DiffLineSection, SectionInfo: &gitdiff.DiffLineSectionInfo{RightIdx: 90}},
			{Type: gitdiff.DiffLineSection, SectionInfo: &gitdiff.DiffLineSectionInfo{RightIdx: 0}},
		}},
	}}
	assert.Equal(t, []uint32{14, 90}, ascHunkStarts(f))
}

func TestAscDraftOf(t *testing.T) {
	tr := func(key string, _ ...any) string { return key }
	assert.True(t, ascDraftOf(&ascv1.DraftContextResponse{NothingToCompare: true}, tr).Nothing)

	d := ascDraftOf(&ascv1.DraftContextResponse{
		Messages:   []string{"Docs and metrics", "Backoff with jitter", "Add retry policy type"},
		Files:      []*ascv1.ChangeFileStat{{Path: []byte("src/client.rs"), Added: 84, Removed: 12}},
		Added:      84,
		Removed:    12,
		Changesets: 3,
		Risk:       &ascv1.ChangeRisk{Level: ascv1.RiskLevel_RISK_LEVEL_MEDIUM, Reasons: []string{"touches billing"}},
	}, tr)
	assert.Equal(t, "Add retry policy type", d.Title, "several changesets: the oldest names the change")
	assert.Contains(t, d.Body, "- Backoff with jitter")
	assert.Contains(t, d.Body, "`src/client.rs` (+84 -12)")
	assert.Contains(t, d.Body, "- touches billing")
	assert.Contains(t, d.Body, "asc_review.risk_medium")

	one := ascDraftOf(&ascv1.DraftContextResponse{Messages: []string{"Fix the refund timeout"}}, tr)
	assert.Equal(t, "Fix the refund timeout", one.Title)
}

func TestAscSuggestedReviewersOffered(t *testing.T) {
	// an agent is not a person of the hub, and nobody is offered when the hub offers nobody
	assert.Empty(t, ascSuggestedReviewersOf(&ascv1.SuggestReviewersResponse{Reviewers: []*ascv1.SuggestedReviewer{{Handle: "coder-2", IsAgent: true}}}, nil))
}

func TestAscVersionCompareOf(t *testing.T) {
	c := ascVersionCompareOf(2, 4, &ascv1.CompareVersionsResponse{
		Changed: 1, Unchanged: 2, Added: 2, Removed: 1,
		Approvals: []*ascv1.ApprovalInfo{
			{Reviewer: "priya", State: ascv1.ApprovalState_APPROVAL_STATE_APPROVED, Applies: true, Carried: true, UnchangedFiles: 2},
			{Reviewer: "tomas", State: ascv1.ApprovalState_APPROVAL_STATE_DISMISSED},
		},
	})
	assert.EqualValues(t, 3, c.Total)
	assert.Len(t, c.Approvals, 1, "a dismissed verdict is not shown")
	assert.True(t, c.Approvals[0].Carried)
}
