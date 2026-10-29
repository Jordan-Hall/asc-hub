// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	"testing"

	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/services/ascagentui"
	"forgejo.org/services/asctrust"

	"github.com/stretchr/testify/assert"
)

func TestAscGateStatsOf(t *testing.T) {
	s := ascGateStatsOf(&ascv1.GetGateStatsResponse{
		Checked: 40, PassedFirstTime: 30, Held: 10, HeldOpen: 2, HeldCleared: 8, MedianClearSeconds: 600, OverridesActive: 1,
		PerDay: []*ascv1.GateDay{{Day: "2026-10-01", Checked: 10, Held: 2}, {Day: "garbage", Checked: 99}, {Day: "2026-10-02", Checked: 5, Held: 5}},
		PerRule: []*ascv1.GateRuleCount{{Rule: "two approvals\x1b[31m", Checked: 40, Held: 7}, {Rule: ""}},
	})
	assert.Equal(t, 75, s.FirstPct)
	assert.Equal(t, "10 min", s.Median)
	assert.Len(t, s.Days, 2, "a day that is not a date is dropped")
	assert.Equal(t, 100, s.Days[0].CheckedPct)
	assert.Equal(t, 50, s.Days[1].CheckedPct)
	assert.Len(t, s.Rules, 1)
	assert.NotContains(t, s.Rules[0].Rule, "\x1b")
	assert.Equal(t, uint32(1), s.Active)
}

func TestAscGateOverridesOf(t *testing.T) {
	change := ascagentui.ChangeBytes("kkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkk")
	out := ascGateOverridesOf([]*ascv1.GateOverride{
		{Id: 1, Change: change, Rule: "two approvals", Reason: "release freeze", GrantedBy: "qa", GrantedAt: 1_700_000_000, Proof: []byte{0xab, 0xcd, 0xef, 1, 2, 3, 4}},
		{Id: 2, Change: change, Rule: "x", RevokedAt: 5, RevokedBy: "qa"},
		{Id: 0, Change: change},
		{Id: 3, Change: []byte{1, 2}},
	})
	assert.Len(t, out, 2, "a row without an id or a real change id is never drawn")
	assert.True(t, out[0].Active)
	assert.Equal(t, "abcdef010203", out[0].Proof)
	assert.False(t, out[1].Active)
}

func TestAscOpsBoardFiltersAndCounts(t *testing.T) {
	rows := []asctrust.OpRow{
		{Seq: 4, ViaKey: "push", Actor: "qa"}, {Seq: 3, ViaKey: "restack", Actor: "asc"},
		{Seq: 2, ViaKey: "push", Actor: "qa"}, {Seq: 1, ViaKey: "agent", Actor: "coder-2"},
	}
	p := &ascOpsPage{OpsURL: "/o/r/asc/ops", Mine: true}
	got := ascOpsBoard(p, "push", rows)
	assert.Len(t, got, 2)
	assert.Equal(t, 4, p.Total)
	counts := map[string]int{}
	for _, c := range p.Chips {
		counts[c.Key] = c.Count
		assert.Contains(t, c.URL, "mine=1", "the other filters are kept")
	}
	assert.Equal(t, 2, counts["push"])
	assert.Equal(t, 1, counts["restack"])
	assert.Equal(t, "qa", p.Actors[0].Actor)
	assert.Equal(t, 100, p.Actors[0].Pct)
	assert.False(t, ascOpsKindKnown("../x"))
	assert.True(t, ascOpsKindKnown("land"))
}

func TestAscQueueTimingOf(t *testing.T) {
	raw := ascagentui.ChangeBytes("kkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkk")
	tm, ok := ascQueueTimingOf(&ascv1.QueueEntry{
		Change: raw, State: ascv1.QueueEntryState_QUEUE_ENTRY_STATE_REVALIDATING, Position: 1,
		Timing: &ascv1.QueueEntryTiming{EtaSeconds: 180, Stage: "checking", ProgressPercent: 240, Detail: "unit running\x1b[31m on merged trunk"},
	})
	assert.True(t, ok)
	assert.Equal(t, "3 min", tm.ETA)
	assert.Equal(t, uint32(100), tm.Percent, "progress is capped")
	assert.NotContains(t, tm.Detail, "\x1b")
	_, ok = ascQueueTimingOf(&ascv1.QueueEntry{Change: []byte{1}})
	assert.False(t, ok)
	tm, _ = ascQueueTimingOf(&ascv1.QueueEntry{Change: raw, State: ascv1.QueueEntryState_QUEUE_ENTRY_STATE_WAITING, Timing: &ascv1.QueueEntryTiming{Stage: "<script>"}})
	assert.Empty(t, tm.Stage, "a stage outside the fixed set is not passed on")
}

func TestAscWorkflowFilter(t *testing.T) {
	defs := []ascWorkflowDef{
		{Name: "review", Valid: true, Enabled: true, Description: "Review every change", Path: ".asc/workflows/review.toml"},
		{Name: "nightly", Valid: true, Description: "Nightly dependency check"},
		{Name: "broken", Valid: false},
	}
	c := ascWorkflowCounts(defs)
	assert.Equal(t, ascWorkflowCount{All: 3, On: 1, Off: 1, Invalid: 1}, c)
	assert.Len(t, ascWorkflowFilter(defs, "", ""), 3)
	assert.Equal(t, "review", ascWorkflowFilter(defs, "on", "")[0].Name)
	assert.Equal(t, "nightly", ascWorkflowFilter(defs, "off", "")[0].Name)
	assert.Equal(t, "broken", ascWorkflowFilter(defs, "invalid", "")[0].Name)
	assert.Len(t, ascWorkflowFilter(defs, "", "DEPENDENCY"), 1, "the search ignores case and reads the description")
	assert.Empty(t, ascWorkflowFilter(defs, "on", "nightly"))
	assert.Equal(t, []string{"a", "b"}, ascWorkflowChain([]ascWorkflowStep{{ID: "a"}, {ID: "b"}}))
}

func TestAscQueueTarget(t *testing.T) {
	assert.Equal(t, "refs/heads/main", ascQueueTarget("main"))
	assert.Equal(t, "refs/heads/feature/x", ascQueueTarget("feature/x"))
	assert.Equal(t, "refs/heads/main", ascQueueTarget("refs/heads/main"))
}
