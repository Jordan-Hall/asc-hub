// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package asctrust

import (
	"strings"
	"testing"
	"time"

	ascv1 "forgejo.org/modules/ascproto/ascv1"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var t0 = time.Date(2026, 3, 10, 9, 0, 0, 0, time.UTC)

func TestBuildPullChecksFailingForOneReason(t *testing.T) {
	in := PullChecksIn{
		Version: 4, Head: "9b2e41cdeadbeefcafe",
		Statuses: []StatusIn{
			{Context: "Unit tests", State: "failure", Description: "40 pass, 2 fail", URL: "/acme/payments/actions/runs/7"},
			{Context: "Lint", State: "success"},
			{Context: "Build (linux, windows)", State: "success"},
			{Context: "Integration", State: "success", Description: "javascript:alert(1)", URL: "javascript:alert(1)"},
		},
		Required: []string{"Unit tests", "Lint", "Security scan"},
		Runs: []RunIn{
			{ID: 7, URL: "/acme/payments/actions/runs/1", State: "failure", Created: t0, Started: t0.Add(time.Second), Stopped: t0.Add(3*time.Minute + 13*time.Second)},
		},
		MainStatuses: []StatusIn{{Context: "Unit tests", State: "success"}},
		Causes: []CauseIn{{
			Group:       GroupRow{ID: 4, Title: "the client panics on a zero retry delay", TestName: "retry::tests::zero_attempts", StateKey: "open", State: "Open", FirstSeen: t0.Add(time.Minute)},
			Occurrences: []OccurrenceRow{{RunID: 7, Message: "thread panicked at src/payments/retry.rs:12:9:\nattempt to divide by zero"}},
		}},
		CausesKnown: true, Now: t0.Add(time.Hour),
	}
	pc := BuildPullChecks(in)
	assert.Equal(t, "1 check failed, for 1 reason", pc.Headline)
	assert.Equal(t, "no", pc.Tone)
	assert.Equal(t, "Failing", pc.Pill)
	assert.Equal(t, "9b2e41cdea", pc.Head)
	assert.Equal(t, []string{"Security scan"}, pc.Stale, "a required check with no result on this version")
	assert.Equal(t, "3 min 12 s", pc.Took)
	assert.Equal(t, 1, pc.FailedRuns)

	require.Len(t, pc.Causes, 1)
	c := pc.Causes[0]
	assert.Equal(t, 1, c.Index)
	assert.Equal(t, 1, c.Tests)
	assert.True(t, c.IsNew, "first seen after this version's first run started")
	assert.Contains(t, c.Excerpt, "attempt to divide by zero")
	assert.Equal(t, "/acme/payments/actions/runs/1", c.RunURL)

	// The list: failures first, then the stale required check, then the rest.
	var names []string
	for _, s := range pc.Statuses {
		names = append(names, s.Context)
	}
	assert.Equal(t, "Unit tests", names[0])
	assert.Equal(t, "Security scan", names[1])
	stale := pc.Statuses[1]
	assert.True(t, stale.Stale)
	assert.Equal(t, "No result for this version", stale.Label, "a missing result is never shown as passing")
	assert.True(t, pc.Statuses[0].Required)
	for _, s := range pc.Statuses {
		if s.Context == "Integration" {
			assert.Empty(t, s.URL, "a javascript: link is dropped")
		}
	}
	assert.Equal(t, "Main is green.", pc.Main)
	assert.Equal(t, "ok", pc.MainTone)
}

func TestBuildPullChecksHeadlines(t *testing.T) {
	cases := []struct {
		name     string
		in       PullChecksIn
		headline string
		tone     string
	}{
		{"all passed", PullChecksIn{Statuses: []StatusIn{{Context: "a", State: "success"}, {Context: "b", State: "success"}}}, "All checks passed", "ok"},
		{"none reported", PullChecksIn{}, "No checks have reported for this version", "ro"},
		{"running", PullChecksIn{Statuses: []StatusIn{{Context: "a", State: "pending"}}}, "Checks are running", "wn"},
		{"a run is queued", PullChecksIn{Statuses: []StatusIn{{Context: "a", State: "success"}}, Runs: []RunIn{{ID: 1, State: "waiting"}}}, "Checks are running", "wn"},
		{"waiting for required", PullChecksIn{Statuses: []StatusIn{{Context: "a", State: "success"}}, Required: []string{"a", "b", "c"}}, "Waiting for 2 required checks on this version", "wn"},
		{"two failed", PullChecksIn{Statuses: []StatusIn{{Context: "a", State: "failure"}, {Context: "b", State: "error"}}}, "2 checks failed", "no"},
		{"failed, causes known", PullChecksIn{
			Statuses:    []StatusIn{{Context: "a", State: "failure"}, {Context: "b", State: "failure"}},
			CausesKnown: true,
			Causes: []CauseIn{
				{Group: GroupRow{ID: 1, Title: "x", StateKey: "open"}, Occurrences: []OccurrenceRow{{RunID: 1}}},
				{Group: GroupRow{ID: 2, Title: "y", StateKey: "open"}, Occurrences: []OccurrenceRow{{RunID: 1}}},
			},
		}, "2 checks failed, for 2 reasons", "no"},
	}
	for _, c := range cases {
		pc := BuildPullChecks(c.in)
		assert.Equal(t, c.headline, pc.Headline, c.name)
		assert.Equal(t, c.tone, pc.Tone, c.name)
	}
}

func TestBuildPullChecksRequiredChecksMatchByGlob(t *testing.T) {
	pc := BuildPullChecks(PullChecksIn{
		Statuses: []StatusIn{{Context: "ci / build (linux)", State: "success"}, {Context: "ci / build (windows)", State: "success"}},
		Required: []string{"ci / build *"},
	})
	assert.Empty(t, pc.Stale)
	assert.Equal(t, "All checks passed", pc.Headline)
	for _, s := range pc.Statuses {
		assert.True(t, s.Required, s.Context)
	}
}

func TestBuildPullChecksFlakyAndQuarantine(t *testing.T) {
	strip := []StripCell{{Tone: "ok"}, {Tone: "wn"}}
	pc := BuildPullChecks(PullChecksIn{
		Statuses: []StatusIn{{Context: "Integration", State: "success"}},
		Runs:     []RunIn{{ID: 9, State: "success", Created: t0, Started: t0, Stopped: t0.Add(time.Minute)}},
		Causes: []CauseIn{
			{Group: GroupRow{ID: 1, TestName: "refund_webhook_timeout", Flaky: true, StateKey: "open"}, Occurrences: []OccurrenceRow{{RunID: 9}}, History: &History{Flips: 6, Runs: 20, Cells: strip}},
			{Group: GroupRow{ID: 2, TestName: "quarantined_one", StateKey: "quarantined"}, Occurrences: []OccurrenceRow{{RunID: 9}}},
			{Group: GroupRow{ID: 3, TestName: "not_in_this_version"}, Occurrences: nil},
		},
		CausesKnown: true,
	})
	require.Len(t, pc.Flaky, 1)
	f := pc.Flaky[0]
	assert.Equal(t, "refund_webhook_timeout", f.Test)
	assert.EqualValues(t, 6, f.Flips)
	assert.Equal(t, strip, f.Strip)
	assert.Equal(t, "title=Flaky+test%3A+refund_webhook_timeout", f.IssueQuery)
	assert.Empty(t, pc.Causes, "a flaky or quarantined failure is not a reason, and a group without a hit here is not shown")
	assert.Equal(t, "All checks passed", pc.Headline)
}

func TestBuildPullChecksMain(t *testing.T) {
	failing := BuildPullChecks(PullChecksIn{MainStatuses: []StatusIn{{State: "failure"}, {State: "success"}}})
	assert.Equal(t, "Main is failing: 1 check failed there too.", failing.Main)
	assert.Equal(t, "no", failing.MainTone)
	assert.Equal(t, "Main has no results.", BuildPullChecks(PullChecksIn{MainStatuses: []StatusIn{}}).Main)
	assert.Equal(t, "Main is still running its checks.", BuildPullChecks(PullChecksIn{MainStatuses: []StatusIn{{State: "pending"}}}).Main)
	assert.Equal(t, "Unknown", BuildPullChecks(PullChecksIn{}).Main)
}

func TestBuildPullChecksTextIsCleaned(t *testing.T) {
	pc := BuildPullChecks(PullChecksIn{
		Statuses: []StatusIn{{Context: "bad\u202ename\x00", State: "failure", Description: "x\ny"}},
		Causes:   []CauseIn{{Group: GroupRow{ID: 1, Title: "t", StateKey: "open"}, Occurrences: []OccurrenceRow{{RunID: 1, Message: "\x1bline"}}}},
	})
	assert.Equal(t, "badname", pc.Statuses[0].Context)
	assert.Equal(t, "x y", pc.Statuses[0].Description)
	assert.NotContains(t, pc.Causes[0].Excerpt, "\x1b")
}

func TestSafeURL(t *testing.T) {
	for in, want := range map[string]string{
		"/acme/payments/actions/runs/1": "/acme/payments/actions/runs/1",
		"https://ci.example.com/run/1":  "https://ci.example.com/run/1",
		"http://ci.example.com/run/1":   "http://ci.example.com/run/1",
		"":                              "",
		"javascript:alert(1)":           "",
		"data:text/html,<script>":       "",
		"//evil.example.com/x":          "",
		"/ok but space":                 "",
		"vbscript:x":                    "",
		"https://x/\u0000":              "",
		"/" + strings.Repeat("a", 3000): "",
		"  /trimmed  ":                  "/trimmed",
		"ftp://example.com/x":           "",
	} {
		assert.Equal(t, want, safeURL(in), "%q", in)
	}
}

func TestDuration(t *testing.T) {
	assert.Equal(t, "41 s", duration(41*time.Second))
	assert.Equal(t, "3 min 12 s", duration(3*time.Minute+12*time.Second))
	assert.Equal(t, "1 h 5 min", duration(time.Hour+5*time.Minute+10*time.Second))
	assert.Equal(t, "0 s", duration(0))
}

func TestQueryEscape(t *testing.T) {
	assert.Equal(t, "a+b%26c%3D%23%25", queryEscape("a b&c=#%"))
	assert.Equal(t, "plain-text_1.0~", queryEscape("plain-text_1.0~"))
	assert.Equal(t, "%C3%A9", queryEscape("\u00e9"))
}

func TestPullCausesKeepsTheGroupsSeenInTheseRuns(t *testing.T) {
	f, c := startFake(t)
	f.listGroups = func(req *ascv1.ListFailureGroupsRequest) (*ascv1.ListFailureGroupsResponse, error) {
		return &ascv1.ListFailureGroupsResponse{Groups: []*ascv1.FailureGroup{
			{Id: 1, Title: "mine", TestName: "t1", State: ascv1.FailureGroupState_FAILURE_GROUP_STATE_OPEN, Flaky: true},
			{Id: 2, Title: "somebody else's", TestName: "t2", State: ascv1.FailureGroupState_FAILURE_GROUP_STATE_OPEN},
		}}, nil
	}
	f.getGroup = func(req *ascv1.GetFailureGroupRequest) (*ascv1.GetFailureGroupResponse, error) {
		run := uint64(100)
		if req.GetId() == 2 {
			run = 555
		}
		return &ascv1.GetFailureGroupResponse{
			Group:       &ascv1.FailureGroup{Id: req.GetId(), Title: "x", TestName: "t", Flaky: req.GetId() == 1},
			Occurrences: []*ascv1.FailureOccurrence{{RunId: run, Message: "boom"}, {RunId: 999}},
		}, nil
	}
	f.history = func(*ascv1.FlakyHistoryRequest) (*ascv1.FlakyHistoryResponse, error) {
		return &ascv1.FlakyHistoryResponse{Buckets: []*ascv1.FlakyBucket{{Start: day(1), Runs: 1}}}, nil
	}
	since := t0
	got, err := PullCauses(System(t.Context()), c, "acme/payments", map[uint64]bool{100: true}, since)
	require.NoError(t, err)
	require.Len(t, got, 1, "the group seen only in other runs is left out")
	assert.EqualValues(t, 1, got[0].Group.ID)
	require.Len(t, got[0].Occurrences, 1)
	assert.EqualValues(t, 100, got[0].Occurrences[0].RunID)
	assert.NotNil(t, got[0].History, "a flaky group comes with its history")

	req := f.last(t, "ListFailureGroups").Req.(*ascv1.ListFailureGroupsRequest)
	assert.Equal(t, since.Add(-causeSinceSlack*time.Second).Unix(), req.GetSince())
	assert.Contains(t, req.GetStates(), ascv1.FailureGroupState_FAILURE_GROUP_STATE_QUARANTINED)
	assert.EqualValues(t, maxCauses, req.GetLimit())

	none, err := PullCauses(System(t.Context()), c, "acme/payments", nil, since)
	require.NoError(t, err)
	assert.Empty(t, none, "no runs, no call")
}
