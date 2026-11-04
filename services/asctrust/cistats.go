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

// MainCompare is how the newest finished run of a change compares with the newest finished run of the default branch
// (Ci.CompareWithMain): the failing tests of the change, how many of them do not fail on main, and the slowest test now
// against the same test on main.
type MainCompare struct {
	Failing     uint32
	New         uint32
	SlowestJob  string
	SlowestTest string
	// SlowestMs is the time of the slowest test; SlowestMainMs its time on main, 0 when main has no such test.
	SlowestMs     uint64
	SlowestMainMs uint64
}

// LoadMainCompare asks asc how the change compares with main. It returns nil when the change has no finished run with
// test results.
func LoadMainCompare(ctx context.Context, conn grpc.ClientConnInterface, repoName string, change []byte) (*MainCompare, error) {
	cctx, cancel := context.WithTimeout(ctx, CallTimeout)
	defer cancel()
	resp, err := ascv1.NewCiClient(conn).CompareWithMain(cctx, &ascv1.CompareWithMainRequest{Repo: RepoRef(repoName), Change: change})
	if err != nil {
		return nil, err
	}
	return MainCompareOf(resp), nil
}

// MainCompareOf turns an answer of Ci.CompareWithMain into a MainCompare: nil when nothing was compared; the names are
// cleaned and the new failures are at most the failing ones.
func MainCompareOf(r *ascv1.CompareWithMainResponse) *MainCompare {
	if !r.GetCompared() {
		return nil
	}
	c := &MainCompare{Failing: r.GetFailing(), New: min(r.GetNewFailureCount(), r.GetFailing())}
	if t := r.GetSlowestTest(); t != nil {
		c.SlowestTest = CleanText(t.GetName(), maxNameBytes)
		if c.SlowestTest != "" {
			c.SlowestJob = CleanText(t.GetJob(), maxNameBytes)
			c.SlowestMs, c.SlowestMainMs = t.GetDurationMs(), t.GetMainDurationMs()
		}
	}
	return c
}

// MillisText writes a test time: "350 ms" under a second, else as DurationText ("41 s").
func MillisText(ms uint64) string {
	if ms < 1000 {
		return itoa(int(ms)) + " ms"
	}
	return duration(time.Duration(ms) * time.Millisecond)
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
