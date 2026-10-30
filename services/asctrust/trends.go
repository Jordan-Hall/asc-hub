// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package asctrust

import (
	"context"
	"fmt"
	"sort"

	ascv1 "forgejo.org/modules/ascproto/ascv1"

	"google.golang.org/grpc"
)

// The three views of the CI failures tab: the failing jobs and the failure groups, the flaky tests, and the trend of
// each failing job over a window. Every view reads CiInsights; none of them writes.
const (
	// MaxTrendJobs bounds the jobs whose trend is drawn, and so the calls one view of the trends makes.
	MaxTrendJobs = 8
	// maxJobStats bounds the rows of the failing jobs table.
	maxJobStats = 12
	// jobScanGroups is how many failure groups are read to find the failing jobs.
	jobScanGroups = 100
)

// JobStat is what the failure groups say about one job: how many causes, how many failures and in how many changes.
type JobStat struct {
	// Job is the job name; "" collects the groups that name no job.
	Job         string
	Groups      int
	Occurrences uint32
	// Changes is the largest number of changes any one group of the job touched (the groups can overlap, so they are
	// not added).
	Changes uint32
	Flaky   int
	// Share is the failures of the job as a percentage of the failures of the job with the most, for the bar.
	Share uint32
}

// JobStats groups failure groups by the job they happened in, the job with the most failures first, at most
// maxJobStats rows. It is the "failing steps" table: where in the pipeline the failures are.
func JobStats(rows []GroupRow) []JobStat {
	byJob := map[string]*JobStat{}
	for _, r := range rows {
		st := byJob[r.JobName]
		if st == nil {
			st = &JobStat{Job: r.JobName}
			byJob[r.JobName] = st
		}
		st.Groups++
		st.Occurrences += r.Occurrences
		st.Changes = max(st.Changes, r.ChangesAffected)
		if r.Flaky {
			st.Flaky++
		}
	}
	out := make([]JobStat, 0, len(byJob))
	var most uint32
	for _, st := range byJob {
		most = max(most, st.Occurrences)
		out = append(out, *st)
	}
	for i := range out {
		if most > 0 {
			out[i].Share = out[i].Occurrences * 100 / most
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Occurrences != out[j].Occurrences {
			return out[i].Occurrences > out[j].Occurrences
		}
		return out[i].Job < out[j].Job
	})
	if len(out) > maxJobStats {
		out = out[:maxJobStats]
	}
	return out
}

// FlakyPage is a page of the flaky tests.
type FlakyPage struct {
	Rows []FlakyRow
	Next string
}

// LoadFlaky reads one page of the tests that flipped between pass and fail in the window.
func LoadFlaky(ctx context.Context, conn grpc.ClientConnInterface, repoName string, windowDays uint32, pageToken string) (*FlakyPage, error) {
	if pageToken != "" && !tokenPattern.MatchString(pageToken) {
		return nil, fmt.Errorf("%w: page token", ErrBadInput)
	}
	cctx, cancel := context.WithTimeout(ctx, CallTimeout)
	defer cancel()
	resp, err := ascv1.NewCiInsightsClient(conn).ListFlakyTests(cctx, &ascv1.ListFlakyTestsRequest{
		Repo: RepoRef(repoName), WindowDays: windowDays, Limit: 50, PageToken: pageToken,
	})
	if err != nil {
		return nil, err
	}
	rows, next, err := FlakyOf(resp)
	if err != nil {
		return nil, err
	}
	return &FlakyPage{Rows: rows, Next: next}, nil
}

// TrendRow is the history of one failing job.
type TrendRow struct {
	Job     string
	History *History
}

// LoadTrends reads the failure history of the jobs that have open or acknowledged failure groups: the jobs with
// the most failures first, at most MaxTrendJobs. A job whose history cannot be read is left out, because the
// trend is advice; if no history can be read at all and there were jobs, the last error is returned.
func LoadTrends(ctx context.Context, conn grpc.ClientConnInterface, repoName string, windowDays uint32) ([]TrendRow, error) {
	client := ascv1.NewCiInsightsClient(conn)
	cctx, cancel := context.WithTimeout(ctx, CallTimeout)
	defer cancel()
	gresp, err := client.ListFailureGroups(cctx, &ascv1.ListFailureGroupsRequest{
		Repo:   RepoRef(repoName),
		States: []ascv1.FailureGroupState{ascv1.FailureGroupState_FAILURE_GROUP_STATE_OPEN, ascv1.FailureGroupState_FAILURE_GROUP_STATE_ACKNOWLEDGED},
		Limit:  jobScanGroups,
	})
	if err != nil {
		return nil, err
	}
	groups, err := GroupsPageOf(gresp)
	if err != nil {
		return nil, err
	}
	var jobs []string
	for _, st := range JobStats(groups.Rows) {
		if st.Job != "" {
			jobs = append(jobs, st.Job)
		}
		if len(jobs) == MaxTrendJobs {
			break
		}
	}
	var out []TrendRow
	var lastErr error
	for _, job := range jobs {
		hresp, err := client.FlakyHistory(cctx, &ascv1.FlakyHistoryRequest{Repo: RepoRef(repoName), JobName: job, WindowDays: windowDays})
		if err != nil {
			lastErr = err
			continue
		}
		h, err := HistoryOf(hresp)
		if err != nil {
			lastErr = err
			continue
		}
		out = append(out, TrendRow{Job: job, History: h})
	}
	if len(out) == 0 && lastErr != nil {
		return nil, lastErr
	}
	return out, nil
}
