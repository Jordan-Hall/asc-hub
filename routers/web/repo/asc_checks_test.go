// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	"net/http"
	"net/url"
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

func TestAscChecksStateTabsAreFourWithTheOpenTabCurrent(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	f := startFakeTrust(t, asctrust.FeatureCiInsights)
	f.listGroups = func(*ascv1.ListFailureGroupsRequest) (*ascv1.ListFailureGroupsResponse, error) {
		return &ascv1.ListFailureGroupsResponse{Groups: []*ascv1.FailureGroup{
			checksGroup(1, "panic", "unit", 7, ascv1.FailureGroupState_FAILURE_GROUP_STATE_OPEN),
		}}, nil
	}
	page := checksPage(t, "", 2)

	require.Equal(t, http.StatusOK, page.status, page.body)
	for _, key := range []string{"open", "acknowledged", "quarantined", "resolved"} {
		assert.Equal(t, 1, strings.Count(page.body, `data-testid="asc-checks-tab-`+key+`"`), key)
	}
	assert.NotContains(t, page.body, "asc_trust.checks.state.</a>", "no tab is labelled with half a translation key")
	assert.Equal(t, 4, strings.Count(page.body, `data-testid="asc-checks-tab-`), "the default view is the open tab, not a fifth one")
	open := page.body[strings.Index(page.body, `data-testid="asc-checks-tab-open"`)-200:]
	assert.Contains(t, open[:300], `aria-current="page"`, "the default view is the open tab")
}

func checksGroups(groups ...*ascv1.FailureGroup) func(*ascv1.ListFailureGroupsRequest) (*ascv1.ListFailureGroupsResponse, error) {
	return func(*ascv1.ListFailureGroupsRequest) (*ascv1.ListFailureGroupsResponse, error) {
		return &ascv1.ListFailureGroupsResponse{Groups: groups}, nil
	}
}

func checksDetail(f *fakeTrust) {
	f.getGroup = func(r *ascv1.GetFailureGroupRequest) (*ascv1.GetFailureGroupResponse, error) {
		return &ascv1.GetFailureGroupResponse{
			Group: checksGroup(r.GetId(), "panic", "unit", 7, ascv1.FailureGroupState_FAILURE_GROUP_STATE_OPEN),
			Occurrences: []*ascv1.FailureOccurrence{
				{RunId: 1 << 40, Attempt: 2, At: 1_700_000_000, Message: "running 3 tests\nthread panicked at src/lib.rs:9\nnote: run with RUST_BACKTRACE=1\n"},
				{RunId: 1 << 40, Attempt: 1, At: 1_699_000_000},
			},
		}, nil
	}
}

func checksWriter(t *testing.T, query string) trustPage {
	t.Helper()
	return runTrust(t, trustReq{path: "/user2/repo1/asc/checks" + query, user: 2, access: perm_model.AccessModeWrite}, AscChecks)
}

func TestAscChecksFirstGroupOpensWithItsLogAndWhereItHappened(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	f := startFakeTrust(t, asctrust.FeatureCiInsights)
	open := ascv1.FailureGroupState_FAILURE_GROUP_STATE_OPEN
	f.listGroups = checksGroups(checksGroup(4, "panic", "unit", 7, open), checksGroup(5, "timeout", "e2e", 2, open))
	checksDetail(f)
	page := checksPage(t, "", 2)

	require.Equal(t, http.StatusOK, page.status, page.body)
	assert.Equal(t, 1, strings.Count(page.body, `data-testid="asc-group-open"`), "only the first group is open")
	assert.Contains(t, page.body, `id="asc-group-4"`)
	assert.Contains(t, page.body, `<span class="rv-ln" aria-hidden="true">2</span><span class="rv-lt">thread panicked at src/lib.rs:9</span>`)
	assert.Equal(t, 3, strings.Count(page.body, `class="rv-ln"`), "the trailing new line is not a line")
	assert.Contains(t, page.body, `data-testid="asc-group-where"`)
	assert.NotContains(t, page.body, `data-testid="asc-group-all"`, "both occurrences are shown")
	assert.NotContains(t, page.body, `data-testid="asc-group-do"`, "a reader has no actions")
	assert.NotContains(t, page.body, `data-testid="asc-group-ack"`)
	calls := f.callsOf("GetFailureGroup")
	require.Len(t, calls, 1)
	req := calls[0].Req.(*ascv1.GetFailureGroupRequest)
	assert.EqualValues(t, 4, req.GetId())
	assert.EqualValues(t, ascOpenOccurrences+1, req.GetLimit())
	assert.Contains(t, page.body, "open=0", "the open group has a link that closes it")
	assert.Contains(t, page.body, "open=5", "another group has a link that opens it")
}

func TestAscChecksOpenParameterPicksTheGroup(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	f := startFakeTrust(t, asctrust.FeatureCiInsights)
	open := ascv1.FailureGroupState_FAILURE_GROUP_STATE_OPEN
	f.listGroups = checksGroups(checksGroup(4, "panic", "unit", 7, open), checksGroup(5, "timeout", "e2e", 2, open))
	checksDetail(f)

	page := checksPage(t, "?open=0", 2)
	require.Equal(t, http.StatusOK, page.status, page.body)
	assert.NotContains(t, page.body, `data-testid="asc-group-open"`)
	assert.Empty(t, f.callsOf("GetFailureGroup"), "no group is read when none is open")

	more := f.getGroup
	f.getGroup = func(r *ascv1.GetFailureGroupRequest) (*ascv1.GetFailureGroupResponse, error) {
		resp, err := more(r)
		resp.NextPageToken = "n_2"
		return resp, err
	}
	page = checksPage(t, "?open=5", 2)
	require.Equal(t, http.StatusOK, page.status, page.body)
	assert.Contains(t, page.body, `class="rv-group is-open" id="asc-group-5"`)
	assert.EqualValues(t, 5, f.callsOf("GetFailureGroup")[0].Req.(*ascv1.GetFailureGroupRequest).GetId())
	assert.Contains(t, page.body, `data-testid="asc-group-all"`, "the group page has the occurrences that do not fit")

	f.getGroup = func(*ascv1.GetFailureGroupRequest) (*ascv1.GetFailureGroupResponse, error) {
		return nil, status.Error(codes.Internal, "boom")
	}
	page = checksPage(t, "?open=5", 2)
	require.Equal(t, http.StatusOK, page.status, "the list still shows when the open group cannot be read")
	assert.NotContains(t, page.body, `data-testid="asc-group-open"`)
	assert.Contains(t, page.body, `data-testid="asc-groups"`)
}

func TestAscChecksWriterRowsHaveActionsAndQuarantineOnlyWhenThePolicyAllowsIt(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	f := startFakeTrust(t, asctrust.FeatureCiInsights)
	open := ascv1.FailureGroupState_FAILURE_GROUP_STATE_OPEN
	f.listGroups = checksGroups(checksGroup(4, "panic", "unit", 7, open), checksGroup(5, "timeout", "e2e", 2, ascv1.FailureGroupState_FAILURE_GROUP_STATE_RESOLVED))
	checksDetail(f)

	page := checksWriter(t, "")
	require.Equal(t, http.StatusOK, page.status, page.body)
	assert.Contains(t, page.body, `data-testid="asc-group-ack"`)
	assert.Contains(t, page.body, `data-testid="asc-group-more"`)
	assert.Equal(t, 1, strings.Count(page.body, `data-testid="asc-group-resolve"`), "only the open group can be resolved")
	assert.Equal(t, 1, strings.Count(page.body, `data-testid="asc-group-reopen"`), "only the resolved group can be reopened")
	assert.Contains(t, page.body, `data-testid="asc-group-do"`)
	assert.Contains(t, page.body, `name="next" value="checks"`)
	assert.NotContains(t, page.body, `data-testid="asc-group-quarantine-row"`, "no policy allows quarantine")
	assert.NotContains(t, page.body, `data-testid="asc-group-do-quarantine"`)

	f.policySource = func(*ascv1.GetPolicySourceRequest) (*ascv1.GetPolicySourceResponse, error) {
		return &ascv1.GetPolicySourceResponse{Present: true, Text: []byte("[ci]\nallow_quarantine = true\n"), Parsed: &ascv1.GetPolicyResponse{Present: true}}, nil
	}
	page = checksWriter(t, "?open=4&act=quarantined")
	require.Equal(t, http.StatusOK, page.status, page.body)
	assert.Contains(t, page.body, `data-testid="asc-group-quarantine-row"`)
	assert.Contains(t, page.body, `rv-btn-primary rv-btn-sm" type="submit" name="state" value="quarantined"`, "the action of the link is the main button")
	assert.Contains(t, page.body, `rv-btn-secondary rv-btn-sm" type="submit" name="state" value="acknowledged"`)
	assert.Contains(t, page.body, "autofocus", "the note is asked for")

	reader := checksPage(t, "?open=4&act=quarantined", 2)
	assert.NotContains(t, reader.body, `data-testid="asc-group-quarantine-row"`, "a reader has no actions")
}

func TestAscChecksEmptyStates(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	f := startFakeTrust(t, asctrust.FeatureCiInsights)
	f.listGroups = checksGroups()

	page := checksPage(t, "", 2)
	require.Equal(t, http.StatusOK, page.status, page.body)
	assert.Contains(t, page.body, "asc_trust.checks.nothing_title")
	assert.Contains(t, page.body, "asc_trust.checks.see_trends")
	assert.Contains(t, page.body, "view=trends")
	assert.NotContains(t, page.body, `data-testid="asc-groups-clear"`)

	page = checksPage(t, "?job=unit", 2)
	require.Equal(t, http.StatusOK, page.status, page.body)
	assert.Contains(t, page.body, "asc_trust.checks.match_none_title")
	assert.Contains(t, page.body, `data-testid="asc-groups-clear"`)
	assert.NotContains(t, page.body, "asc_trust.checks.nothing_title")

	page = checksPage(t, "?state=resolved", 2)
	require.Equal(t, http.StatusOK, page.status, page.body)
	assert.Contains(t, page.body, "asc_trust.checks.groups_empty")
	assert.NotContains(t, page.body, "asc_trust.checks.nothing_title")
}

func TestAscChecksLoadingAndRetry(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	f := startFakeTrust(t, asctrust.FeatureCiInsights)
	f.listGroups = checksGroups()
	page := checksPage(t, "", 2)
	assert.Contains(t, page.body, `data-testid="asc-checks-loading"`)
	assert.Contains(t, page.body, "data-asc-list-main")
	assert.Contains(t, page.body, "data-asc-list-filter")
	assert.NotContains(t, page.body, `data-testid="asc-checks-retry"`)

	f.listGroups = func(*ascv1.ListFailureGroupsRequest) (*ascv1.ListFailureGroupsResponse, error) {
		return nil, status.Error(codes.Internal, "boom")
	}
	page = checksPage(t, "", 2)
	assert.Equal(t, http.StatusBadGateway, page.status)
	assert.Contains(t, page.body, "asc_trust.checks.err_title.failures")
	assert.Contains(t, page.body, `data-testid="asc-checks-retry"`)
	assert.NotContains(t, page.body, `data-testid="asc-checks-loading"`, "a page that failed has no list to load")

	page = checksPage(t, "?window=5", 2)
	assert.Equal(t, http.StatusBadRequest, page.status)
	assert.NotContains(t, page.body, `data-testid="asc-checks-retry"`, "trying a bad request again does not help")
}

func TestAscCheckGroupStateReturnsToTheList(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	f := startFakeTrust(t, asctrust.FeatureCiInsights)
	f.setGroup = func(r *ascv1.SetFailureGroupStateRequest) (*ascv1.SetFailureGroupStateResponse, error) {
		return &ascv1.SetFailureGroupStateResponse{Group: checksGroup(r.GetId(), "panic", "unit", 7, r.GetState())}, nil
	}
	post := func(form url.Values) trustPage {
		return runTrust(t, trustReq{
			method: http.MethodPost, path: "/user2/repo1/asc/checks/groups/4/state", user: 2, access: perm_model.AccessModeWrite,
			form: form, params: map[string]string{":id": "4"},
		}, AscCheckGroupState)
	}

	page := post(url.Values{"state": {"acknowledged"}, "note": {"known"}, "op": {asctrust.NewOpID()}, "next": {"checks"}, "back": {"state=open&job=unit&evil=1"}})
	assert.Equal(t, "/user2/repo1/asc/checks?job=unit&open=4&state=open#asc-group-4", page.loc, "only the keys of the list are kept")
	req := f.callsOf("SetFailureGroupState")[0].Req.(*ascv1.SetFailureGroupStateRequest)
	assert.Equal(t, ascv1.FailureGroupState_FAILURE_GROUP_STATE_ACKNOWLEDGED, req.GetState())
	assert.Equal(t, "known", req.GetNote())

	page = post(url.Values{"state": {"resolved"}, "op": {asctrust.NewOpID()}})
	assert.Equal(t, "/user2/repo1/asc/checks/groups/4", page.loc, "without next the group page is shown")
}

func TestAscLogLinesKeepsTheLastLines(t *testing.T) {
	assert.Nil(t, ascLogLines("", 3))
	assert.Nil(t, ascLogLines("\n\n", 3))
	assert.Equal(t, []ascLogLine{{1, "a"}, {2, "b"}}, ascLogLines("a\r\nb\n", 3))
	assert.Equal(t, []ascLogLine{{3, "c"}, {4, "d"}}, ascLogLines("a\nb\nc\nd", 2))
}

func TestAscListBackKeepsOnlyTheListKeys(t *testing.T) {
	assert.Empty(t, ascListBack("%zz"))
	assert.Equal(t, url.Values{"state": {"open"}, "flaky": {"1"}, "page": {"n_2"}}, ascListBack("state=open&flaky=1&page=n_2&open=9&redirect=//evil"))
	assert.Empty(t, ascListBack("test="+strings.Repeat("x", 513)))
}
