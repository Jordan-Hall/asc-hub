// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package ascagentui

import (
	"strings"
	"testing"
	"time"

	ascv1 "forgejo.org/modules/ascproto/ascv1"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunStates(t *testing.T) {
	active := map[ascv1.AgentRunState]bool{
		ascv1.AgentRunState_AGENT_RUN_STATE_QUEUED:          true,
		ascv1.AgentRunState_AGENT_RUN_STATE_RUNNING:         true,
		ascv1.AgentRunState_AGENT_RUN_STATE_WAITING_INPUT:   true,
		ascv1.AgentRunState_AGENT_RUN_STATE_SUCCEEDED:       false,
		ascv1.AgentRunState_AGENT_RUN_STATE_FAILED:          false,
		ascv1.AgentRunState_AGENT_RUN_STATE_CANCELLED:       false,
		ascv1.AgentRunState_AGENT_RUN_STATE_BUDGET_EXCEEDED: false,
		ascv1.AgentRunState_AGENT_RUN_STATE_EXPIRED:         false,
	}
	for s, want := range active {
		st := RunState(s)
		assert.Equal(t, want, st.Active, s.String())
		assert.NotEmpty(t, st.Label, s.String())
		assert.NotEmpty(t, st.Slug, s.String())
		assert.Contains(t, []string{"ok", "run", "wait", "bad", "warn", "idle"}, st.Tone)
	}
	// Every state the proto defines is described, and an unknown one is finished so a page never polls forever.
	for v := range ascv1.AgentRunState_name {
		if v == 0 {
			continue
		}
		_, ok := active[ascv1.AgentRunState(v)]
		assert.True(t, ok, "state %d has no entry", v)
	}
	unknown := RunState(ascv1.AgentRunState(99))
	assert.False(t, unknown.Active)
	assert.Equal(t, "Unknown", unknown.Label)
	assert.False(t, RunState(ascv1.AgentRunState_AGENT_RUN_STATE_UNSPECIFIED).Active)
}

func TestRunStateFilter(t *testing.T) {
	states, ok := RunStateFilter("")
	assert.True(t, ok)
	assert.Empty(t, states)

	states, ok = RunStateFilter("active")
	assert.True(t, ok)
	assert.ElementsMatch(t, []ascv1.AgentRunState{
		ascv1.AgentRunState_AGENT_RUN_STATE_QUEUED, ascv1.AgentRunState_AGENT_RUN_STATE_RUNNING, ascv1.AgentRunState_AGENT_RUN_STATE_WAITING_INPUT,
	}, states)

	for _, f := range RunStateSlugs {
		got, ok := RunStateFilter(f.Slug)
		assert.True(t, ok, f.Slug)
		assert.NotEmpty(t, got, f.Slug)
	}
	_, ok = RunStateFilter("nonsense")
	assert.False(t, ok)
	_, ok = RunStateFilter("RUNNING")
	assert.False(t, ok, "slugs are lower case")
}

func TestWorkflowStates(t *testing.T) {
	for v := range ascv1.WorkflowRunState_name {
		if v == 0 {
			continue
		}
		assert.NotEqual(t, "Unknown", WorkflowRunState(ascv1.WorkflowRunState(v)).Label, "run state %d", v)
	}
	for v := range ascv1.WorkflowStepState_name {
		if v == 0 {
			continue
		}
		assert.NotEqual(t, "Unknown", StepState(ascv1.WorkflowStepState(v)).Label, "step state %d", v)
	}
	for v := range ascv1.AgentTokenState_name {
		if v == 0 {
			continue
		}
		assert.NotEqual(t, "Unknown", TokenState(ascv1.AgentTokenState(v)).Label, "token state %d", v)
	}
	assert.True(t, WorkflowRunState(ascv1.WorkflowRunState_WORKFLOW_RUN_STATE_WAITING_APPROVAL).Active)
	assert.False(t, WorkflowRunState(ascv1.WorkflowRunState_WORKFLOW_RUN_STATE_FAILED).Active)
	assert.False(t, WorkflowRunState(ascv1.WorkflowRunState(77)).Active)
	assert.True(t, StepState(ascv1.WorkflowStepState_WORKFLOW_STEP_STATE_PENDING).Active)

	states, ok := WorkflowRunStateFilter("active")
	assert.True(t, ok)
	assert.Len(t, states, 3)
	_, ok = WorkflowRunStateFilter("bogus")
	assert.False(t, ok)

	assert.Equal(t, "Approval", StepKind(ascv1.WorkflowStepKind_WORKFLOW_STEP_KIND_APPROVAL))
	assert.Equal(t, "Step", StepKind(ascv1.WorkflowStepKind(40)))
	for v := range ascv1.WorkflowTriggerKind_name {
		if v == 0 {
			continue
		}
		assert.NotEqual(t, "Unknown trigger", TriggerLabel(ascv1.WorkflowTriggerKind(v)), "trigger %d", v)
	}
	for v := range ascv1.AgentRunEventKind_name {
		if v == 0 {
			continue
		}
		word, _ := EventKind(ascv1.AgentRunEventKind(v))
		assert.NotEqual(t, "Event", word, "event kind %d", v)
	}
}

func TestChangeCodec(t *testing.T) {
	raw := []byte{0x00, 0x0f, 0xf0, 0xff, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12}
	text := ChangeText(raw)
	require.Len(t, text, 32)
	assert.Equal(t, "zzzkkzkk", text[:8], "the nibble 0 is z and 15 is k")
	assert.Equal(t, raw, ChangeBytes(text))
	all := make([]byte, 16)
	for i := range all {
		all[i] = 0xff
	}
	assert.Equal(t, strings.Repeat("k", 32), ChangeText(all))

	assert.Empty(t, ChangeText(nil))
	assert.Empty(t, ChangeText(make([]byte, 15)))
	assert.Empty(t, ChangeText(make([]byte, 32)), "a commit id is not a change id")
	assert.Nil(t, ChangeBytes("short"))
	assert.Nil(t, ChangeBytes("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"), "letters outside k to z")
	assert.Nil(t, ChangeBytes(text+"z"))
	assert.Equal(t, text[:12], ShortChange(text))
	assert.Equal(t, "abc", ShortChange("abc"))
}

func TestTimes(t *testing.T) {
	assert.True(t, Time(0).IsZero())
	assert.True(t, Time(-5).IsZero())
	assert.True(t, Time(1<<40).IsZero(), "a value outside a sane range is no time")
	assert.Equal(t, time.Date(2026, 10, 26, 10, 0, 0, 0, time.UTC), Time(time.Date(2026, 10, 26, 10, 0, 0, 0, time.UTC).Unix()))
	assert.True(t, TimeMs(0).IsZero())
	assert.True(t, TimeMs(1<<50).IsZero())
	assert.Equal(t, int64(1_700_000_000_123), TimeMs(1_700_000_000_123).UnixMilli())
}
