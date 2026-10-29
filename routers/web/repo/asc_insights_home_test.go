// Copyright 2026 The ASC Hub Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	"testing"

	ascv1 "forgejo.org/modules/ascproto/ascv1"

	"github.com/stretchr/testify/assert"
)

func TestHomeAuthorsOf(t *testing.T) {
	a := homeAuthorsOf(&ascv1.InsightsStatsResponse{Landed: 46, LandedByPeople: 21, LandedByAgents: 25, AgentChangesReviewed: 25, MedianReviewSeconds: 18 * 60}, 30)
	assert.Equal(t, 46, a.PeoplePct, "21 of 46 is 46 percent")
	assert.Equal(t, 54, a.AgentsPct)
	assert.Equal(t, 100, a.ReviewedPct)
	assert.EqualValues(t, 18, a.MedianMinutes)

	b := homeAuthorsOf(&ascv1.InsightsStatsResponse{LandedByPeople: 3, LandedByAgents: 4, AgentChangesReviewed: 9}, 30)
	assert.Equal(t, 100, b.ReviewedPct, "reviewed is never more than the agent changes")
	assert.Zero(t, b.MedianMinutes)

	assert.Nil(t, homeAuthorsOf(&ascv1.InsightsStatsResponse{}, 30), "nothing landed: no card")
	assert.Nil(t, homeAuthorsOf(nil, 30))

	c := homeAuthorsOf(&ascv1.InsightsStatsResponse{LandedByPeople: 5}, 30)
	assert.Equal(t, 100, c.PeoplePct)
	assert.Zero(t, c.AgentsPct)
	assert.Zero(t, c.ReviewedPct, "no agent change: nothing to review")
}

func TestHomeContributorsOf(t *testing.T) {
	r := &ascv1.ListContributorsResponse{Contributors: []*ascv1.Contributor{
		{Name: "priya", Landed: 61, Kind: ascv1.AuthorKind_AUTHOR_KIND_PERSON},
		{Name: "coder-2", Landed: 33, Kind: ascv1.AuthorKind_AUTHOR_KIND_AGENT},
		{Name: "reviewer", Landed: 0},
		{Name: "", Landed: 5},
		{Name: "diego", Landed: 30},
	}}
	got := homeContributorsOf(r, 2)
	assert.Len(t, got, 2)
	assert.Equal(t, "priya", got[0].Name)
	assert.Equal(t, 100, got[0].Pct)
	assert.True(t, got[1].Agent, "an agent is marked")
	assert.Equal(t, 54, got[1].Pct)
	assert.Empty(t, homeContributorsOf(nil, 5))
}
