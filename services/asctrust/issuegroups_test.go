// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package asctrust

import (
	"net/url"
	"testing"

	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/test"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAnIssueNamesTheGroupsOfItsRepositoryByAddress(t *testing.T) {
	t.Cleanup(test.MockVariableValue(&setting.AppURL, "https://hub.example.org/"))
	t.Cleanup(test.MockVariableValue(&setting.AppSubURL, ""))
	assert.Equal(t, "https://hub.example.org/acme/payments/asc/checks/groups/42", GroupURL("acme", "payments", 42))

	text := "Failure group: https://hub.example.org/acme/payments/asc/checks/groups/42\n" +
		"also [here](/Acme/Payments/asc/checks/groups/7) and again https://hub.example.org/acme/payments/asc/checks/groups/42"
	assert.Equal(t, []uint64{42, 7}, GroupRefs(text, "acme", "payments"))

	for name, text := range map[string]string{
		"another repository":    "https://hub.example.org/acme/other/asc/checks/groups/3",
		"another host":          "https://elsewhere.example.org/acme/payments/asc/checks/groups/3",
		"a longer owner":        "https://hub.example.org/xacme/payments/asc/checks/groups/3",
		"no id":                 "/acme/payments/asc/checks/groups/",
		"an id that runs on":    "/acme/payments/asc/checks/groups/3abc",
		"zero":                  "/acme/payments/asc/checks/groups/0",
		"too long":              "/acme/payments/asc/checks/groups/123456789012345678901",
		"inside another path":   "/mirror/acme/payments/asc/checks/groups/3",
		"no text":               "",
		"only the checks page":  "/acme/payments/asc/checks",
		"a group of a group id": "/acme/payments/asc/checks/groups/x3",
	} {
		assert.Empty(t, GroupRefs(text, "acme", "payments"), name)
	}

	many := ""
	for _, id := range []string{"1", "2", "3", "4", "5", "6"} {
		many += " /acme/payments/asc/checks/groups/" + id
	}
	assert.Len(t, GroupRefs(many, "acme", "payments"), MaxGroupRefsPerIssue)
}

func TestAGroupPathUnderASubPath(t *testing.T) {
	t.Cleanup(test.MockVariableValue(&setting.AppURL, "https://example.org/hub/"))
	t.Cleanup(test.MockVariableValue(&setting.AppSubURL, "/hub"))
	assert.Equal(t, []uint64{9}, GroupRefs("see https://example.org/hub/acme/payments/asc/checks/groups/9.", "acme", "payments"))
	assert.Equal(t, []uint64{9}, GroupRefs("(/hub/acme/payments/asc/checks/groups/9)", "acme", "payments"))
	assert.Empty(t, GroupRefs("/acme/payments/asc/checks/groups/9", "acme", "payments"), "the path of the hub starts with its sub path")
}

func TestTheIssueQueryOfAGroupCarriesItsAddress(t *testing.T) {
	q, err := url.ParseQuery(string(GroupIssueQuery("Failing check: a & b", "Failure group: https://hub.example.org/acme/payments/asc/checks/groups/42")))
	require.NoError(t, err)
	assert.Equal(t, "Failing check: a & b", q.Get("title"))
	assert.Equal(t, "Failure group: https://hub.example.org/acme/payments/asc/checks/groups/42", q.Get("body"))
}

func quarantined(id uint64) *ascv1.FailureGroup {
	return &ascv1.FailureGroup{Id: id, Title: "boom", TestName: "t", State: ascv1.FailureGroupState_FAILURE_GROUP_STATE_QUARANTINED}
}

func TestLoadQuarantinedOfPagesUntilEveryWantedGroupIsFound(t *testing.T) {
	f, c := startFake(t)
	f.listGroups = func(req *ascv1.ListFailureGroupsRequest) (*ascv1.ListFailureGroupsResponse, error) {
		if req.GetPageToken() == "" {
			return &ascv1.ListFailureGroupsResponse{Groups: []*ascv1.FailureGroup{quarantined(1), quarantined(2)}, NextPageToken: "n_2"}, nil
		}
		return &ascv1.ListFailureGroupsResponse{Groups: []*ascv1.FailureGroup{quarantined(3)}, NextPageToken: "n_3"}, nil
	}
	got, err := LoadQuarantinedOf(System(t.Context()), c, "acme/payments", map[uint64]bool{2: true, 3: true})
	require.NoError(t, err)
	assert.Len(t, got, 2)
	assert.Equal(t, 2, f.count("ListFailureGroups"), "it stops once both are found")
	req := f.last(t, "ListFailureGroups").Req.(*ascv1.ListFailureGroupsRequest)
	assert.Equal(t, []ascv1.FailureGroupState{ascv1.FailureGroupState_FAILURE_GROUP_STATE_QUARANTINED}, req.GetStates())
	assert.Equal(t, "n_2", req.GetPageToken())

	n := f.count("ListFailureGroups")
	got, err = LoadQuarantinedOf(System(t.Context()), c, "acme/payments", map[uint64]bool{99: true})
	require.NoError(t, err)
	assert.Empty(t, got, "a group that is not quarantined has no chip")
	assert.LessOrEqual(t, f.count("ListFailureGroups")-n, maxQuarantinePages, "the pages are bounded")

	n = f.count("ListFailureGroups")
	got, err = LoadQuarantinedOf(System(t.Context()), c, "acme/payments", nil)
	require.NoError(t, err)
	assert.Empty(t, got)
	assert.Equal(t, n, f.count("ListFailureGroups"), "nothing wanted, nothing asked")
}

func TestLoadQuarantinedOfRefusesAGroupInAnotherState(t *testing.T) {
	f, c := startFake(t)
	f.listGroups = func(*ascv1.ListFailureGroupsRequest) (*ascv1.ListFailureGroupsResponse, error) {
		return &ascv1.ListFailureGroupsResponse{Groups: []*ascv1.FailureGroup{{Id: 1, Title: "boom", State: ascv1.FailureGroupState_FAILURE_GROUP_STATE_OPEN}}}, nil
	}
	_, err := LoadQuarantinedOf(System(t.Context()), c, "acme/payments", map[uint64]bool{1: true})
	require.ErrorIs(t, err, ErrBadAnswer)
}
