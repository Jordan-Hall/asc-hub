// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package asctrust

import (
	"context"
	"time"

	ascv1 "forgejo.org/modules/ascproto/ascv1"

	"google.golang.org/grpc"
)

// MaxStatsDays is the longest window asc counts statistics over (Ci.Stats).
const MaxStatsDays = 90

// CiStats is the health of the CI of a repository over a window of days: the stat cards of the checks page.
type CiStats struct {
	Days                 uint32
	Runs                 uint32
	Passed               uint32
	PassPermille         uint32
	PreviousRuns         uint32
	PreviousPassPermille uint32
	GroupsOpen           uint32
	GroupsNewHour        uint32
	// Flipping are the tests whose verdict changed on one head; Flaky those of them at or above the threshold.
	Flipping          uint32
	Flaky             uint32
	ThresholdPermille uint32
	MedianSeconds     uint32
	SlowestJob        string
	SlowestJobSeconds uint32
	// Daily is one entry per day of the window, oldest first.
	Daily []CiStatsDay
}

// CiStatsDay is one day of the window of CiStats.
type CiStatsDay struct {
	Runs          uint32
	Passed        uint32
	MedianSeconds uint32
	GroupsNew     uint32
	Flips         uint32
}

// LoadCiStats reads the statistics of the last days (at most MaxStatsDays) from asc.
func LoadCiStats(ctx context.Context, conn grpc.ClientConnInterface, repoName string, days uint32) (*CiStats, error) {
	cctx, cancel := context.WithTimeout(ctx, CallTimeout)
	defer cancel()
	resp, err := ascv1.NewCiClient(conn).Stats(cctx, &ascv1.CiStatsRequest{Repo: RepoRef(repoName), Days: min(days, MaxStatsDays)})
	if err != nil {
		return nil, err
	}
	return CiStatsOf(resp), nil
}

// DurationText writes a number of seconds as the checks pages do: "42 s", "6 min 12 s", "1 h 5 min".
func DurationText(seconds uint32) string {
	return duration(time.Duration(seconds) * time.Second)
}

// CiStatsOf turns an answer of Ci.Stats into CiStats: the job name is cleaned, rates are at most 1000 per mille and
// the days are at most MaxStatsDays.
func CiStatsOf(r *ascv1.CiStatsResponse) *CiStats {
	s := &CiStats{
		Days: r.GetDays(), Runs: r.GetRuns(), Passed: r.GetPassed(), PassPermille: min(r.GetPassPermille(), 1000),
		PreviousRuns: r.GetPreviousRuns(), PreviousPassPermille: min(r.GetPreviousPassPermille(), 1000),
		GroupsOpen: r.GetFailureGroupsOpen(), GroupsNewHour: r.GetFailureGroupsNewHour(),
		Flipping: r.GetFlippingTests(), Flaky: r.GetFlakyTests(), ThresholdPermille: min(r.GetFlakyThresholdPermille(), 1000),
		MedianSeconds: r.GetMedianDurationSeconds(), SlowestJob: CleanText(r.GetSlowestJob(), maxNameBytes),
		SlowestJobSeconds: r.GetSlowestJobSeconds(),
	}
	if s.SlowestJob == "" {
		s.SlowestJobSeconds = 0
	}
	daily := r.GetDaily()
	if len(daily) > MaxStatsDays {
		daily = daily[len(daily)-MaxStatsDays:]
	}
	for _, d := range daily {
		s.Daily = append(s.Daily, CiStatsDay{
			Runs: d.GetRuns(), Passed: min(d.GetPassed(), d.GetRuns()), MedianSeconds: d.GetMedianDurationSeconds(),
			GroupsNew: d.GetFailureGroupsNew(), Flips: d.GetFlakyFlips(),
		})
	}
	return s
}
