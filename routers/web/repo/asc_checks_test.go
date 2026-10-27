// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	"net/http"
	"strings"
	"testing"

	perm_model "forgejo.org/models/perm"
	"forgejo.org/models/unittest"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/services/asctrust"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func checksGroup(id uint64, title, job string, occurrences uint32, state ascv1.FailureGroupState) *ascv1.FailureGroup {
	return &ascv1.FailureGroup{Id: id, Title: title, TestName: "t" + title, JobName: job, State: state, Occurrences: occurrences, ChangesAffected: 2, LastSeen: 1_700_000_000}
}

func checksPage(t *testing.T, query string, user int64) trustPage {
	t.Helper()
	return runTrust(t, trustReq{path: "/user2/repo1/asc/checks" + query, user: user, access: perm_model.AccessModeRead}, AscChecks)
}

func TestAscChecksFailuresViewShowsFailingJobsAndGroups(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	f := startFakeTrust(t, asctrust.FeatureCiInsights)
	f.listGroups = func(r *ascv1.ListFailureGroupsRequest) (*ascv1.ListFailureGroupsResponse, error) {
		open := ascv1.FailureGroupState_FAILURE_GROUP_STATE_OPEN
		return &ascv1.ListFailureGroupsResponse{Groups: []*ascv1.FailureGroup{
			checksGroup(1, "panic", "unit", 7, open), checksGroup(2, "timeout", "e2e", 20, open), checksGroup(3, "lint", "unit", 2, open),
		}, NextPageToken: "n_3"}, nil
	}
	page := checksPage(t, "", 2)

	require.Equal(t, http.StatusOK, page.status, page.body)
	assert.Contains(t, page.body, `data-testid="asc-jobs"`, "the failing jobs table")
	assert.Less(t, strings.Index(page.body, ">e2e</a>"), strings.Index(page.body, ">unit</a>"), "the job with the most failures comes first")
	assert.Contains(t, page.body, `data-testid="asc-groups"`)
	assert.Contains(t, page.body, "/user2/repo1/asc/checks/groups/2")
	assert.Contains(t, page.body, `data-testid="asc-checks-view-failures"`)
	assert.Contains(t, page.body, `data-testid="asc-checks-view-flaky"`)
	assert.Contains(t, page.body, `data-testid="asc-checks-view-trends"`)
	assert.Contains(t, page.body, `aria-current="page"`)
	assert.Contains(t, page.body, `for="asc-checks-job"`, "fields have labels")
	assert.Contains(t, page.body, `rel="next"`)
	assert.Empty(t, f.callsOf("ListFlakyTests"), "the flaky list is read in its own view")
	assert.NotContains(t, page.body, "Failing jobs", "no English literal: text comes from the locale")
}

func TestAscChecksFlakyView(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	f := startFakeTrust(t, asctrust.FeatureCiInsights)
	f.listFlaky = func(r *ascv1.ListFlakyTestsRequest) (*ascv1.ListFlakyTestsResponse, error) {
		return &ascv1.ListFlakyTestsResponse{Tests: []*ascv1.FlakyTest{
			{TestName: "retry::zero", JobName: "unit", FlakeRate: 0.125, Runs: 40, GroupId: 9, LastFlip: 1_700_000_000},
			{TestName: "net::dial", FlakeRate: 0.05, Runs: 20},
		}, NextPageToken: "n_2"}, nil
	}
	page := checksPage(t, "?view=flaky&window=14", 2)

	require.Equal(t, http.StatusOK, page.status, page.body)
	assert.Contains(t, page.body, `data-testid="asc-flaky"`)
	assert.Contains(t, page.body, "retry::zero")
	assert.Contains(t, page.body, "12.5%")
	assert.Contains(t, page.body, "/user2/repo1/asc/checks/groups/9")
	assert.Contains(t, page.body, `page=n_2&amp;view=flaky&amp;window=14`, "the next page keeps the view and the window")
	req := f.callsOf("ListFlakyTests")[0].Req.(*ascv1.ListFlakyTestsRequest)
	assert.EqualValues(t, 14, req.GetWindowDays())
	assert.Empty(t, f.callsOf("ListFailureGroups"))
}

func TestAscChecksTrendsView(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	f := startFakeTrust(t, asctrust.FeatureCiInsights)
	f.listGroups = func(r *ascv1.ListFailureGroupsRequest) (*ascv1.ListFailureGroupsResponse, error) {
		open := ascv1.FailureGroupState_FAILURE_GROUP_STATE_OPEN
		return &ascv1.ListFailureGroupsResponse{Groups: []*ascv1.FailureGroup{
			checksGroup(1, "a", "unit", 7, open), checksGroup(2, "b", "e2e", 20, open), checksGroup(3, "c", "", 99, open),
		}}, nil
	}
	f.history = func(r *ascv1.FlakyHistoryRequest) (*ascv1.FlakyHistoryResponse, error) {
		return &ascv1.FlakyHistoryResponse{Buckets: []*ascv1.FlakyBucket{
			{Start: 1_700_000_000, Runs: 4, Failures: 1}, {Start: 1_700_086_400, Runs: 3},
		}, FlakeRate: 0.02}, nil
	}
	page := checksPage(t, "?view=trends&window=7", 2)

	require.Equal(t, http.StatusOK, page.status, page.body)
	assert.Equal(t, 2, strings.Count(page.body, `data-testid="asc-trend"`), "one strip per named failing job")
	assert.Contains(t, page.body, `data-testid="asc-strip-summary"`)
	assert.Contains(t, page.body, `role="img"`)
	var jobs []string
	for _, c := range f.callsOf("FlakyHistory") {
		r := c.Req.(*ascv1.FlakyHistoryRequest)
		jobs = append(jobs, r.GetJobName())
		assert.EqualValues(t, 7, r.GetWindowDays())
		assert.Empty(t, r.GetTestName())
	}
	assert.Equal(t, []string{"e2e", "unit"}, jobs, "the job with the most failures first; a group without a job has no trend")
}

func TestAscChecksViewsRefuseBadInputAndUnsupportedServers(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	f := startFakeTrust(t, asctrust.FeatureCiInsights)
	for _, q := range []string{"?view=other", "?window=5", "?view=flaky&window=x"} {
		page := checksPage(t, q, 2)
		assert.Equal(t, http.StatusBadRequest, page.status, q)
		assert.Contains(t, page.body, "asc_trust.checks.err_window", q)
	}
	assert.Empty(t, f.calls, "nothing is asked of asc for a bad request")

	f.listFlaky = func(*ascv1.ListFlakyTestsRequest) (*ascv1.ListFlakyTestsResponse, error) {
		return nil, status.Error(codes.Internal, "stack trace at /srv/asc")
	}
	page := checksPage(t, "?view=flaky", 2)
	assert.Equal(t, http.StatusBadGateway, page.status)
	assert.Contains(t, page.body, "asc_trust.err.other")
	assert.NotContains(t, page.body, "stack trace")

	f2 := startFakeTrust(t)
	page = checksPage(t, "?view=trends", 2)
	assert.Equal(t, http.StatusOK, page.status)
	assert.Contains(t, page.body, "asc_trust.checks.unsupported")
	assert.Empty(t, f2.callsOf("ListFailureGroups"))
}
