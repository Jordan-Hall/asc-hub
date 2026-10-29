// Copyright 2026 The ASC Hub Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package repo

import (
	"testing"

	ascv1 "forgejo.org/modules/ascproto/ascv1"

	"github.com/stretchr/testify/assert"
)

func TestApplyInsightsStats(t *testing.T) {
	r := &ascv1.InsightsStatsResponse{
		Landed: 148, LandedPrevious: 132, StacksOpened: 31, StacksOpenedPrevious: 28,
		MedianReviewSeconds: 18*60 + 30, LandedByPeople: 68, LandedByAgents: 80, AgentChangesReviewed: 80,
		PerDay:    []*ascv1.InsightsDay{{Day: 1759276800, People: 3, Agents: 1}, {Day: 1759363200, People: 0, Agents: 2}},
		Weekly:    []*ascv1.InsightsWeek{{WeekStart: 1759276800, Landed: 10, MedianReviewSeconds: 1800}, {WeekStart: 1759881600, Landed: 12}, {WeekStart: 1760486400, Landed: 9, MedianReviewSeconds: 600}},
		BySize:    []*ascv1.InsightsSizeBucket{{Label: "under_50", Changes: 5, MedianReviewSeconds: 360}, {Label: "50_to_300", Changes: 4, MedianReviewSeconds: 1260}, {Label: "unknown", Changes: 2, MedianReviewSeconds: 60}},
		Truncated: true,
	}
	pg := &insPage{Days: 30, Weeks: []insWeek{{Minutes: 99}}}
	applyInsightsStats(pg, r)
	assert.Equal(t, 148, pg.Landed)
	assert.True(t, pg.HasLandedDelta)
	assert.Equal(t, 12, pg.LandedDelta)
	assert.Equal(t, 18, pg.MedianMin)
	assert.True(t, pg.HasMedian)
	assert.Equal(t, 54, pg.AgentPct)
	assert.True(t, pg.AllAgentsApproved)
	assert.Equal(t, 3, pg.TotalPeople)
	assert.Equal(t, 3, pg.TotalAgent)
	assert.Len(t, pg.Bars, 2)
	if assert.Len(t, pg.Weeks, 2, "a week with no median is left out") {
		assert.Equal(t, 30, pg.Weeks[0].Minutes)
		assert.Equal(t, "faster", pg.Trend)
	}
	if assert.Len(t, pg.Sizes, 2, "the unknown bucket is not a size group") {
		assert.Equal(t, "50_300", pg.Sizes[1].Key)
	}
	assert.True(t, pg.Truncated)
}

func TestApplyInsightsStatsAgentsNotAllReviewed(t *testing.T) {
	pg := &insPage{Days: 30}
	applyInsightsStats(pg, &ascv1.InsightsStatsResponse{LandedByPeople: 1, LandedByAgents: 3, AgentChangesReviewed: 2})
	assert.False(t, pg.AllAgentsApproved)
	assert.False(t, pg.HasLandedDelta, "no earlier period, no percentage")
}

func TestInsWeeksOfFoldsDays(t *testing.T) {
	var days []insDay
	for i := range 15 {
		days = append(days, insDay{People: 1, Agents: i % 2})
	}
	w := insWeeksOf(days)
	if assert.Len(t, w, 3) {
		assert.Equal(t, 7, w[0].People)
		assert.Equal(t, 1, w[2].People, "the last run is shorter")
	}
}

func TestInsKind(t *testing.T) {
	assert.Equal(t, ascv1.AuthorKind_AUTHOR_KIND_AGENT, insKind("agents"))
	assert.Equal(t, ascv1.AuthorKind_AUTHOR_KIND_PERSON, insKind("people"))
	assert.Equal(t, ascv1.AuthorKind_AUTHOR_KIND_UNSPECIFIED, insKind("everyone"))
}
