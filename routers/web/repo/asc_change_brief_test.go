// Copyright 2026 The ASC Hub Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package repo

import (
	"testing"

	ascv1 "forgejo.org/modules/ascproto/ascv1"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChangeBriefOfLinksPointersToTheLandedCommit(t *testing.T) {
	b, err := changeBriefOf(&ascv1.GetBriefResponse{
		Brief: &ascv1.ReviewBrief{
			What:     "not shown on the change page",
			RiskNote: "Retries could stampede.",
			WhereToLook: []*ascv1.WhereToLook{
				{Path: []byte("src/client.rs"), Line: 42, Note: "the retry loop"},
				{Path: []byte("docs/a b.md")},
				{Path: []byte{0xff, 0xfe}, Line: 1},
			},
		},
		Risk: &ascv1.ChangeRisk{Level: ascv1.RiskLevel_RISK_LEVEL_MEDIUM, Reasons: []string{"touches the network client"}},
	}, "/o/r", "abc123")
	require.NoError(t, err)
	require.NotNil(t, b)
	assert.Equal(t, []AscBriefWhere{
		{Path: "src/client.rs", Line: 42, Note: "the retry loop", Link: "/o/r/src/commit/abc123/src/client.rs#L42"},
		{Path: "docs/a b.md", Link: "/o/r/src/commit/abc123/docs/a%20b.md"},
	}, b.Where, "a pointer with a bad path is dropped")
	assert.Equal(t, "medium", b.RiskLevel)
	assert.Equal(t, "Retries could stampede.", b.RiskNote)
	assert.Equal(t, []string{"touches the network client"}, b.RiskReasons)
}

func TestChangeBriefOfWithNothingToShow(t *testing.T) {
	b, err := changeBriefOf(&ascv1.GetBriefResponse{Brief: &ascv1.ReviewBrief{What: "only a what"}, Files: 2}, "/o/r", "abc")
	require.NoError(t, err)
	assert.Nil(t, b, "no pointers and no risk: no card")

	b, err = changeBriefOf(&ascv1.GetBriefResponse{Risk: &ascv1.ChangeRisk{Level: ascv1.RiskLevel_RISK_LEVEL_HIGH}}, "/o/r", "abc")
	require.NoError(t, err)
	require.NotNil(t, b, "a risk class alone is shown")
	assert.Equal(t, "high", b.RiskLevel)

	_, err = changeBriefOf(&ascv1.GetBriefResponse{Added: 1 << 30}, "/o/r", "abc")
	require.Error(t, err, "an absurd answer is refused")
}
