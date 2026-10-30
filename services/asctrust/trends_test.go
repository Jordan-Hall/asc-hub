// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package asctrust

import (
	"fmt"
	"testing"

	ascv1 "forgejo.org/modules/ascproto/ascv1"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestJobStatsGroupsByJobAndOrdersByFailures(t *testing.T) {
	rows := []GroupRow{
		{JobName: "unit", Occurrences: 5, ChangesAffected: 2},
		{JobName: "unit", Occurrences: 3, ChangesAffected: 4, Flaky: true},
		{JobName: "e2e", Occurrences: 20, ChangesAffected: 1},
		{JobName: "", Occurrences: 1},
		{JobName: "a", Occurrences: 8},
		{JobName: "b", Occurrences: 8},
	}
	got := JobStats(rows)
	require.Len(t, got, 5)
	assert.Equal(t, JobStat{Job: "e2e", Groups: 1, Occurrences: 20, Changes: 1, Share: 100}, got[0])
	assert.Equal(t, JobStat{Job: "a", Groups: 1, Occurrences: 8, Share: 40}, got[1], "equal failures are ordered by name")
	assert.Equal(t, "b", got[2].Job)
	assert.Equal(t, JobStat{Job: "unit", Groups: 2, Occurrences: 8, Changes: 4, Flaky: 1, Share: 40}, got[3], "changes are the largest of the groups, not their sum")
	assert.Equal(t, "", got[4].Job)

	var many []GroupRow
	for i := range 40 {
		many = append(many, GroupRow{JobName: fmt.Sprintf("job%02d", i), Occurrences: uint32(i)})
	}
	assert.Len(t, JobStats(many), maxJobStats)
	assert.Empty(t, JobStats(nil))
}

func TestLoadTrendsReadsTheHistoryOfTheMostFailingJobs(t *testing.T) {
	f, c := startFake(t)
	f.listGroups = func(r *ascv1.ListFailureGroupsRequest) (*ascv1.ListFailureGroupsResponse, error) {
		assert.Equal(t, []ascv1.FailureGroupState{ascv1.FailureGroupState_FAILURE_GROUP_STATE_OPEN, ascv1.FailureGroupState_FAILURE_GROUP_STATE_ACKNOWLEDGED}, r.GetStates())
		var groups []*ascv1.FailureGroup
		for i := range 12 {
			groups = append(groups, &ascv1.FailureGroup{Id: uint64(i + 1), JobName: fmt.Sprintf("job%02d", i), Occurrences: uint32(100 - i)})
		}
		return &ascv1.ListFailureGroupsResponse{Groups: groups}, nil
	}
	f.history = func(r *ascv1.FlakyHistoryRequest) (*ascv1.FlakyHistoryResponse, error) {
		if r.GetJobName() == "job01" {
			return nil, status.Error(codes.Internal, "boom")
		}
		return &ascv1.FlakyHistoryResponse{Buckets: []*ascv1.FlakyBucket{{Start: 1_700_000_000, Runs: 2, Failures: 1}}, FlakeRate: 0.1}, nil
	}
	rows, err := LoadTrends(userCtx(t, "tok"), c, "o/r", 14)
	require.NoError(t, err)
	require.Len(t, rows, MaxTrendJobs-1, "a job whose history cannot be read is left out")
	assert.Equal(t, "job00", rows[0].Job)
	assert.Equal(t, "job02", rows[1].Job)
	assert.Equal(t, MaxTrendJobs, f.count("FlakyHistory"), "at most MaxTrendJobs calls")
	assert.EqualValues(t, 14, f.last(t, "FlakyHistory").Req.(*ascv1.FlakyHistoryRequest).GetWindowDays())

	// If no history can be read at all, the failure is not hidden.
	f.history = func(*ascv1.FlakyHistoryRequest) (*ascv1.FlakyHistoryResponse, error) {
		return nil, status.Error(codes.Unavailable, "down")
	}
	_, err = LoadTrends(userCtx(t, "tok"), c, "o/r", 14)
	assert.Error(t, err)

	// No failing job: nothing to draw, and no error.
	f.listGroups = func(*ascv1.ListFailureGroupsRequest) (*ascv1.ListFailureGroupsResponse, error) {
		return &ascv1.ListFailureGroupsResponse{}, nil
	}
	rows, err = LoadTrends(userCtx(t, "tok"), c, "o/r", 14)
	require.NoError(t, err)
	assert.Empty(t, rows)
}

func TestLoadFlakyChecksThePageToken(t *testing.T) {
	f, c := startFake(t)
	f.listFlaky = func(r *ascv1.ListFlakyTestsRequest) (*ascv1.ListFlakyTestsResponse, error) {
		return &ascv1.ListFlakyTestsResponse{Tests: []*ascv1.FlakyTest{{TestName: "t", FlakeRate: 0.5, Runs: 4}}, NextPageToken: "n_9"}, nil
	}
	page, err := LoadFlaky(userCtx(t, "tok"), c, "o/r", 30, "n_2")
	require.NoError(t, err)
	assert.Equal(t, "n_9", page.Next)
	require.Len(t, page.Rows, 1)
	assert.Equal(t, "50.0", page.Rows[0].RatePercent)
	assert.Equal(t, "n_2", f.last(t, "ListFlakyTests").Req.(*ascv1.ListFlakyTestsRequest).GetPageToken())

	n := f.count("ListFlakyTests")
	_, err = LoadFlaky(userCtx(t, "tok"), c, "o/r", 30, "bad token!")
	assert.ErrorIs(t, err, ErrBadInput)
	assert.Equal(t, n, f.count("ListFlakyTests"))
}
