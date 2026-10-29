// Copyright 2026 The ASC Hub Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package repo

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestInsMedian(t *testing.T) {
	_, ok := insMedian(nil)
	assert.False(t, ok)
	m, _ := insMedian([]int{9, 1, 5})
	assert.Equal(t, 5, m)
	m, _ = insMedian([]int{4, 10})
	assert.Equal(t, 7, m, "the mean of the two middle values")
}

func TestInsPercentChange(t *testing.T) {
	d, ok := insPercentChange(112, 100)
	assert.True(t, ok)
	assert.Equal(t, 12, d)
	d, _ = insPercentChange(75, 100)
	assert.Equal(t, -25, d)
	_, ok = insPercentChange(5, 0)
	assert.False(t, ok, "no earlier period, no percentage")
}

func TestInsRange(t *testing.T) {
	k, d := insRange("7d")
	assert.Equal(t, "7d", k)
	assert.Equal(t, 7, d)
	k, d = insRange("year")
	assert.Equal(t, "year", k)
	assert.Equal(t, 365, d)
	k, d = insRange("../etc")
	assert.Equal(t, "30d", k, "anything else is the default")
	assert.Equal(t, 30, d)
}

func TestInsBucketsSplitPeopleAndAgents(t *testing.T) {
	start := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	pulls := []*insPull{
		{Merged: start.Add(2 * time.Hour)},
		{Merged: start.Add(3 * time.Hour), Agent: true},
		{Merged: start.Add(26 * time.Hour)},
		{Merged: start.Add(-time.Hour)},          // before the window
		{Merged: start.Add(10 * 24 * time.Hour)}, // after it
		{},                                       // not merged
	}
	days := insBuckets(pulls, start, 5, 24*time.Hour)
	assert.Equal(t, 1, days[0].People)
	assert.Equal(t, 1, days[0].Agents)
	assert.Equal(t, 1, days[1].People)
	assert.Zero(t, days[2].People+days[2].Agents)
}

func TestInsWeeklyAndSizes(t *testing.T) {
	now := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	mk := func(daysAgo int, waitMin int, size int) *insPull {
		c := now.Add(-time.Duration(daysAgo) * 24 * time.Hour)
		return &insPull{Created: c, FirstReview: c.Add(time.Duration(waitMin) * time.Minute), Size: size, HasSize: size > 0}
	}
	pulls := []*insPull{mk(1, 10, 20), mk(2, 30, 20), mk(3, 60, 100), mk(20, 120, 500), {Created: now.Add(-time.Hour)}}
	weeks := insWeekly(pulls, now, 8)
	if assert.NotEmpty(t, weeks) {
		assert.Equal(t, 30, weeks[len(weeks)-1].Minutes, "the median of 10, 30 and 60; the newest week holds those three")
	}
	sizes := insSizes(pulls)
	got := map[string]int{}
	for _, s := range sizes {
		got[s.Key] = s.Minutes
	}
	assert.Equal(t, map[string]int{"under_50": 20, "50_300": 60, "over_300": 120}, got)
}

func TestInsRecentStacksGroupByRoot(t *testing.T) {
	now := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	pulls := []*insPull{
		{ID: 1, Name: "retry", Merged: now.Add(-48 * time.Hour)},
		{ID: 2, Name: "retry-2", Parent: 1, Merged: now.Add(-24 * time.Hour)},
		{ID: 3, Name: "docs", Merged: now.Add(-3 * time.Hour)},
		{ID: 4, Name: "old", Merged: now.Add(-40 * 24 * time.Hour)},
	}
	got := insRecentStacks(pulls, now.AddDate(0, 0, -14), 5)
	if assert.Len(t, got, 2) {
		assert.Equal(t, "docs", got[0].Name)
		assert.Equal(t, "retry", got[1].Name)
		assert.Equal(t, 2, got[1].Changes)
	}
}

func TestInsBarsFillThePlot(t *testing.T) {
	bars := insBars([]insDay{{People: 4}, {People: 1, Agents: 3}, {}}, 4)
	assert.Equal(t, insPlotH, bars[0].PH)
	assert.Equal(t, insChartH-insPlotH, bars[0].PY)
	assert.Equal(t, bars[1].PY-bars[1].AH, bars[1].AY, "agents sit on top of people")
	assert.Zero(t, bars[2].PH+bars[2].AH)
	assert.Nil(t, insBars(nil, 0))
}
