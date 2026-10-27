// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	"net/http"
	"strings"
	"testing"

	perm_model "forgejo.org/models/perm"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unittest"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/services/asctrust"
	"forgejo.org/services/automerge"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func quarantinedGroup(id uint64, title, note string) *ascv1.FailureGroup {
	return &ascv1.FailureGroup{
		Id: id, Title: title, TestName: "retry::tests::zero_delay", JobName: "unit", Note: note,
		State: ascv1.FailureGroupState_FAILURE_GROUP_STATE_QUARANTINED, Occurrences: 7, ChangesAffected: 3, LastSeen: 1_700_000_000,
	}
}

func queuePage(t *testing.T, user int64, noActions bool) trustPage {
	t.Helper()
	return queuePageAs(t, trustReq{user: user, noActions: noActions})
}

func queuePageAs(t *testing.T, r trustReq) trustPage {
	t.Helper()
	t.Cleanup(automerge.UseAscQueueBacked(func(*repo_model.Repository) (bool, error) { return true, nil }))
	r.path, r.access = "/user2/repo1/asc/queue", perm_model.AccessModeRead
	return runTrust(t, r, AscQueuePage)
}

func TestAscQueueShowsQuarantinedChecksAndWhy(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	f := startFakeTrust(t, asctrust.FeatureCiInsights)
	f.listGroups = func(r *ascv1.ListFailureGroupsRequest) (*ascv1.ListFailureGroupsResponse, error) {
		return &ascv1.ListFailureGroupsResponse{
			Groups:        []*ascv1.FailureGroup{quarantinedGroup(4, "the client panics on a zero retry delay", "Known: tracked in the retry rework"), quarantinedGroup(5, "timeout", "")},
			NextPageToken: "n_2",
		}, nil
	}
	page := queuePage(t, 2, false)

	require.Equal(t, http.StatusOK, page.status, page.body)
	assert.Contains(t, page.body, `data-testid="asc-queue-quarantine"`)
	assert.Contains(t, page.body, "asc_trust.queue.quarantine_title")
	assert.Contains(t, page.body, "the client panics on a zero retry delay")
	assert.Contains(t, page.body, "Known: tracked in the retry rework", "the reason written when it was quarantined")
	assert.Contains(t, page.body, "asc_trust.queue.no_note", "a group without a note says so")
	assert.Contains(t, page.body, "/user2/repo1/asc/checks/groups/4")
	assert.Contains(t, page.body, "/user2/repo1/asc/checks?state=quarantined", "more groups than the page lists")

	calls := f.callsOf("ListFailureGroups")
	require.Len(t, calls, 1)
	req := calls[0].Req.(*ascv1.ListFailureGroupsRequest)
	assert.Equal(t, []ascv1.FailureGroupState{ascv1.FailureGroupState_FAILURE_GROUP_STATE_QUARANTINED}, req.GetStates())
	assert.EqualValues(t, asctrust.MaxQuarantined, req.GetLimit())
	assert.Equal(t, "Bearer per-request-token", calls[0].Auth, "a signed-in person reads as that person")
}

func TestAscQueueQuarantineNeedsActionsRead(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	f := startFakeTrust(t, asctrust.FeatureCiInsights)
	page := queuePage(t, 2, true)
	require.Equal(t, http.StatusOK, page.status)
	assert.NotContains(t, page.body, "asc_trust.queue.quarantine_title", "CI data is shown only to a reader of Actions")
	assert.Empty(t, f.callsOf("ListFailureGroups"))
}

// A person who may read the Actions but not the code of the repository must not learn which changes and tests are
// quarantined: the section follows the guard of the checks pages.
func TestAscQueueQuarantineNeedsCodeRead(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	f := startFakeTrust(t, asctrust.FeatureCiInsights)
	f.listGroups = func(*ascv1.ListFailureGroupsRequest) (*ascv1.ListFailureGroupsResponse, error) {
		return &ascv1.ListFailureGroupsResponse{Groups: []*ascv1.FailureGroup{quarantinedGroup(4, "a secret test name", "private note")}}, nil
	}
	page := queuePageAs(t, trustReq{user: 4, noCode: true})
	require.Equal(t, http.StatusOK, page.status, "the queue itself follows the right to read pull requests")
	assert.NotContains(t, page.body, "asc_trust.queue.quarantine_title")
	assert.NotContains(t, page.body, "a secret test name")
	assert.NotContains(t, page.body, "private note")
	assert.Empty(t, f.callsOf("ListFailureGroups"), "nothing is asked of asc")
	assert.Empty(t, f.callsOf("Version"), "not even for the features")
}

func TestAscQueueQuarantineFailureDoesNotStopTheQueue(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	f := startFakeTrust(t, asctrust.FeatureCiInsights)
	f.listGroups = func(*ascv1.ListFailureGroupsRequest) (*ascv1.ListFailureGroupsResponse, error) {
		return nil, status.Error(codes.Internal, "secret internals at /srv/asc")
	}
	page := queuePage(t, 2, false)
	require.Equal(t, http.StatusOK, page.status)
	assert.Contains(t, page.body, `data-testid="asc-queue"`, "the queue is still drawn")
	assert.Contains(t, page.body, `data-testid="asc-queue-quarantine-problem"`)
	assert.Contains(t, page.body, "asc_trust.err.other")
	assert.NotContains(t, page.body, "secret internals")

	// A group that is not quarantined, in an answer to a question about quarantine, is not believed.
	f.listGroups = func(*ascv1.ListFailureGroupsRequest) (*ascv1.ListFailureGroupsResponse, error) {
		g := quarantinedGroup(4, "x", "")
		g.State = ascv1.FailureGroupState_FAILURE_GROUP_STATE_OPEN
		return &ascv1.ListFailureGroupsResponse{Groups: []*ascv1.FailureGroup{g}}, nil
	}
	page = queuePage(t, 2, false)
	assert.Contains(t, page.body, "asc_trust.err.bad_answer")
	assert.False(t, strings.Contains(page.body, `data-testid="asc-queue-quarantine"`))
}

func TestAscQueueQuarantineEmptyAndUnsupported(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	f := startFakeTrust(t, asctrust.FeatureCiInsights)
	f.listGroups = func(*ascv1.ListFailureGroupsRequest) (*ascv1.ListFailureGroupsResponse, error) {
		return &ascv1.ListFailureGroupsResponse{}, nil
	}
	assert.Contains(t, queuePage(t, 0, false).body, `data-testid="asc-queue-quarantine-empty"`, "also for a visitor who may read the Actions")

	f2 := startFakeTrust(t) // a server without CI insights
	page := queuePage(t, 2, false)
	assert.Equal(t, http.StatusOK, page.status)
	assert.NotContains(t, page.body, "asc_trust.queue.quarantine_title")
	assert.Empty(t, f2.callsOf("ListFailureGroups"))
}
