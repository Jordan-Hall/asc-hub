// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package asctrust

import (
	"testing"

	ascv1 "forgejo.org/modules/ascproto/ascv1"

	"github.com/stretchr/testify/assert"
)

func TestCiStatsOf(t *testing.T) {
	daily := make([]*ascv1.CiStatsDay, MaxStatsDays+5)
	daily[len(daily)-1] = &ascv1.CiStatsDay{Runs: 2, Passed: 9, MedianDurationSeconds: 60, FailureGroupsNew: 1, FlakyFlips: 4}
	s := CiStatsOf(&ascv1.CiStatsResponse{
		Days: 90, Runs: 2, PassPermille: 4000, PreviousPassPermille: 1200, FlakyThresholdPermille: 2000,
		SlowestJob: "", SlowestJobSeconds: 30, Daily: daily,
	})
	assert.EqualValues(t, 1000, s.PassPermille)
	assert.EqualValues(t, 1000, s.PreviousPassPermille)
	assert.EqualValues(t, 1000, s.ThresholdPermille)
	assert.Zero(t, s.SlowestJobSeconds, "no job, no time")
	assert.Len(t, s.Daily, MaxStatsDays, "the newest days are kept")
	assert.Equal(t, CiStatsDay{Runs: 2, Passed: 2, MedianSeconds: 60, GroupsNew: 1, Flips: 4}, s.Daily[MaxStatsDays-1])

	assert.Equal(t, "42 s", DurationText(42))
	assert.Equal(t, "6 min 12 s", DurationText(372))
}
