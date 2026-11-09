// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	"net/http"
	"testing"

	issues_model "forgejo.org/models/issues"
	perm_model "forgejo.org/models/perm"
	"forgejo.org/models/unittest"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/setting"
	"forgejo.org/services/asctrust"
	"forgejo.org/services/context"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// quarantineChips runs prepareAscIssueQuarantine for the issues as the person r and returns the chips.
func quarantineChips(t *testing.T, r trustReq, list issues_model.IssueList) map[int64]ascIssueQuarantineChip {
	t.Helper()
	r.path = "/user2/repo1/issues"
	if r.access == perm_model.AccessModeNone {
		r.access = perm_model.AccessModeRead
	}
	page := runTrust(t, r, func(ctx *context.Context) { prepareAscIssueQuarantine(ctx, list) })
	chips, _ := page.ctx.Data["AscIssueQuarantine"].(map[int64]ascIssueQuarantineChip)
	return chips
}

func issuesNaming() issues_model.IssueList {
	return issues_model.IssueList{
		{ID: 1, RepoID: 1, Content: "Failure group: " + asctrust.GroupURL("user2", "repo1", 4)},
		{ID: 2, RepoID: 1, Content: "see /user2/repo1/asc/checks/groups/5 and /user2/repo1/asc/checks/groups/4"},
		{ID: 3, RepoID: 1, Content: "nothing about checks"},
		{ID: 4, RepoID: 2, Content: "/user2/repo1/asc/checks/groups/4"},
	}
}

func TestAnIssueNamingAQuarantinedGroupHasTheChip(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	f := startFakeTrust(t, asctrust.FeatureCiInsights)
	f.listGroups = func(*ascv1.ListFailureGroupsRequest) (*ascv1.ListFailureGroupsResponse, error) {
		return &ascv1.ListFailureGroupsResponse{Groups: []*ascv1.FailureGroup{quarantinedGroup(4, "the client panics on a zero retry delay", "Known: tracked here")}}, nil
	}
	chips := quarantineChips(t, trustReq{user: 2}, issuesNaming())
	require.Len(t, chips, 2)
	assert.Equal(t, "retry::tests::zero_delay", chips[1].Test)
	assert.Equal(t, "Known: tracked here", chips[1].Note)
	assert.Equal(t, setting.AppURL+"user2/repo1/asc/checks/groups/4", chips[1].Link)
	assert.Equal(t, chips[1], chips[2], "group 5 is not quarantined, group 4 is")
	assert.NotContains(t, chips, int64(3))
	assert.NotContains(t, chips, int64(4), "an issue of another repository is not read")
	require.Len(t, f.callsOf("ListFailureGroups"), 1, "one read for the page")
	req := f.callsOf("ListFailureGroups")[0].Req.(*ascv1.ListFailureGroupsRequest)
	assert.Equal(t, []ascv1.FailureGroupState{ascv1.FailureGroupState_FAILURE_GROUP_STATE_QUARANTINED}, req.GetStates())
}

func TestTheQuarantineChipNeedsAReaderOfTheActionsAndAGroup(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	f := startFakeTrust(t, asctrust.FeatureCiInsights)
	f.listGroups = func(*ascv1.ListFailureGroupsRequest) (*ascv1.ListFailureGroupsResponse, error) {
		return &ascv1.ListFailureGroupsResponse{Groups: []*ascv1.FailureGroup{quarantinedGroup(4, "panic", "")}}, nil
	}
	assert.Empty(t, quarantineChips(t, trustReq{user: 2, noActions: true}, issuesNaming()), "CI data is for readers of the Actions")
	assert.Empty(t, quarantineChips(t, trustReq{user: 2}, issues_model.IssueList{{ID: 3, RepoID: 1, Content: "plain"}}))
	assert.Empty(t, f.callsOf("ListFailureGroups"), "asc is asked only when an issue names a group")
}

func TestTheQuarantineChipNeverFailsTheList(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	f := startFakeTrust(t, asctrust.FeatureCiInsights)
	f.listGroups = func(*ascv1.ListFailureGroupsRequest) (*ascv1.ListFailureGroupsResponse, error) {
		return &ascv1.ListFailureGroupsResponse{Groups: []*ascv1.FailureGroup{{Id: 4, Title: "boom", State: ascv1.FailureGroupState_FAILURE_GROUP_STATE_OPEN}}}, nil
	}
	assert.Empty(t, quarantineChips(t, trustReq{user: 2}, issuesNaming()), "an answer that cannot be trusted shows no chip")

	startFakeTrust(t)
	assert.Empty(t, quarantineChips(t, trustReq{user: 2}, issuesNaming()), "an asc without CI insights shows no chip")
}

func TestAnIssueOpenedFromAGroupNamesTheGroup(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	f := startFakeTrust(t, asctrust.FeatureCiInsights)
	open := ascv1.FailureGroupState_FAILURE_GROUP_STATE_OPEN
	f.listGroups = checksGroups(checksGroup(4, "panic", "unit", 7, open))
	checksDetail(f)
	page := runTrust(t, trustReq{path: "/user2/repo1/asc/checks", user: 2, access: perm_model.AccessModeWrite}, AscChecks)
	require.Equal(t, http.StatusOK, page.status, page.body)
	assert.Contains(t, page.body, `data-testid="asc-group-do-issue"`)
	assert.Contains(t, page.body, "body=asc_trust.checks.issue_body", "the new issue is filled in with the address of the group")
}
