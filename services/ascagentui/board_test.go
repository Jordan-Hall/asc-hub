// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package ascagentui

import (
	"testing"
	"time"

	ascv1 "forgejo.org/modules/ascproto/ascv1"

	"github.com/stretchr/testify/assert"
)

func TestParseDecision(t *testing.T) {
	d, k, ok := ParseDecision("allow")
	assert.True(t, ok)
	assert.Equal(t, ascv1.AgentDecision_AGENT_DECISION_ALLOW_ONCE, d)
	assert.Equal(t, ascv1.AgentRequestKind_AGENT_REQUEST_KIND_PERMISSION, k)
	_, k, ok = ParseDecision("approve")
	assert.True(t, ok)
	assert.Equal(t, ascv1.AgentRequestKind_AGENT_REQUEST_KIND_PLAN, k)
	for _, bad := range []string{"", "ALLOW", "allow ", "delete", "1"} {
		_, _, ok = ParseDecision(bad)
		assert.False(t, ok, bad)
	}
}

func TestRequestOfRefusesMalformedRows(t *testing.T) {
	good := &ascv1.AgentRequest{Id: 1, RunId: 2, Kind: ascv1.AgentRequestKind_AGENT_REQUEST_KIND_PLAN, State: ascv1.AgentRequestState_AGENT_REQUEST_STATE_PENDING,
		Summary: "Plan\x1b[31m", Agent: "docs-1", Plan: []*ascv1.AgentPlanStep{{Text: "one", State: "weird"}, {Text: "two", State: "done"}}}
	r, ok := RequestOf(good)
	assert.True(t, ok)
	assert.True(t, r.Pending)
	assert.Equal(t, "plan", r.Kind)
	assert.NotContains(t, r.Summary, "\x1b")
	assert.Equal(t, "d1", r.Initials)
	assert.Equal(t, "todo", r.Steps[0].State, "an unknown step state is drawn as todo")
	assert.Equal(t, "done", r.Steps[1].State)

	for _, bad := range []*ascv1.AgentRequest{nil, {}, {Id: 1}, {Id: 1, RunId: 2}, {Id: 1, RunId: 2, Kind: ascv1.AgentRequestKind_AGENT_REQUEST_KIND_PLAN}} {
		_, ok := RequestOf(bad)
		assert.False(t, ok)
	}
	assert.Len(t, Requests([]*ascv1.AgentRequest{good, {}, good}), 2)
	perms, plans := RequestCounts(Requests([]*ascv1.AgentRequest{good}))
	assert.Equal(t, 0, perms)
	assert.Equal(t, 1, plans)
}

func TestPercentAndProgress(t *testing.T) {
	assert.Equal(t, 0, Percent(5, 0))
	assert.Equal(t, 58, Percent(116000, 200000))
	assert.Equal(t, 100, Percent(7, 3))
	p := ProgressOf(&ascv1.AgentRunProgress{StepsDone: 3, StepsTotal: 4})
	assert.True(t, p.Known)
	assert.Equal(t, []bool{true, true, true, false}, p.Segments)
	assert.False(t, ProgressOf(&ascv1.AgentRunProgress{StepsDone: 5}).Known, "without a plan there is no total")
	big := ProgressOf(&ascv1.AgentRunProgress{StepsDone: 10, StepsTotal: 20})
	assert.Len(t, big.Segments, 8)
}

func TestSpendOf(t *testing.T) {
	s := SpendOf(&ascv1.GetAgentSpendResponse{
		Month: "2026-10", CostMicroUsd: 142_000_000, MonthlyCapMicroUsd: 400_000_000,
		Days:   []*ascv1.SpendDay{{Day: "2026-10-01", CostMicroUsd: 2_000_000}, {Day: "not a day", CostMicroUsd: 9}, {Day: "2026-10-02", CostMicroUsd: 4_000_000}},
		Agents: []*ascv1.AgentSpend{{Agent: "coder-2", CostMicroUsd: 58_000_000, MonthlyCapMicroUsd: 120_000_000}, {Agent: "BAD NAME"}},
	})
	assert.Equal(t, "$142.00", s.Cost)
	assert.Equal(t, "$400.00", s.Cap)
	assert.Equal(t, 35, s.CapPct)
	assert.False(t, s.Over)
	assert.Len(t, s.Days, 2, "a day that is not a date is dropped")
	assert.Equal(t, 100, s.Days[1].Pct)
	assert.NotEmpty(t, s.Spark)
	assert.Len(t, s.Agents, 1)
	assert.Equal(t, 48, s.Agents[0].CapPct)
	assert.True(t, SpendOf(&ascv1.GetAgentSpendResponse{CostMicroUsd: 5, MonthlyCapMicroUsd: 5}).Over)
	assert.Equal(t, "", SpendOf(&ascv1.GetAgentSpendResponse{Month: "2026-13"}).Month)
}

func TestRosterOfAndCheckpoints(t *testing.T) {
	r := RosterOf([]*ascv1.RepoAgentInfo{
		{Agent: "coder-2", Active: 1, Paths: []string{"src/**", "tests/**"}, Landed_30D: 11},
		{Agent: "reviewer-1", PendingRequests: 1},
		{Agent: "../etc"},
	})
	assert.Len(t, r, 2)
	assert.Equal(t, "running", r[0].Status)
	assert.Equal(t, "src/**, tests/**", r[0].Scope)
	assert.Equal(t, "waiting", r[1].Status)
	assert.Equal(t, "read-only", r[1].Scope)

	cps := CheckpointsOf([]*ascv1.AgentCheckpoint{
		{Id: 1, RunId: 9, Seq: 1, Label: "first", Tip: []byte{0xab, 0xcd, 0xef, 0x01, 0x23, 0x45, 0x67}},
		{Id: 2, RunId: 9, Seq: 2, Label: "second", RolledBackAt: 5, RolledBackBy: "qa"},
		{Id: 3, RunId: 77, Seq: 1, Label: "another run"},
	}, 9)
	assert.Len(t, cps, 2)
	assert.Equal(t, "second", cps[0].Label, "newest first")
	assert.True(t, cps[0].RolledBack)
	assert.Equal(t, "abcdef0123", cps[1].Tip)
}

func TestMonthDay(t *testing.T) {
	assert.Equal(t, 28, MonthDayOf(time.Date(2026, 10, 28, 23, 59, 0, 0, time.UTC)))
}
