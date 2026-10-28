// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package ascagentui

import (
	"strings"
	"time"

	issues_model "forgejo.org/models/issues"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
)

// State is how a run, a token or a step is drawn: a short label and a tone. The tone picks the colour class
// (`asc-tone-<tone>`); the label is always shown too, so colour is never the only signal.
type State struct {
	Slug   string // the lower-case name used in query strings and class names
	Label  string
	Tone   string // ok, run, wait, bad, warn or idle
	Active bool   // the run still changes: poll it
}

var runStates = map[ascv1.AgentRunState]State{
	ascv1.AgentRunState_AGENT_RUN_STATE_QUEUED:          {"queued", "Queued", "idle", true},
	ascv1.AgentRunState_AGENT_RUN_STATE_RUNNING:         {"running", "Running", "run", true},
	ascv1.AgentRunState_AGENT_RUN_STATE_WAITING_INPUT:   {"waiting", "Needs you", "wait", true},
	ascv1.AgentRunState_AGENT_RUN_STATE_SUCCEEDED:       {"succeeded", "Done", "ok", false},
	ascv1.AgentRunState_AGENT_RUN_STATE_FAILED:          {"failed", "Failed", "bad", false},
	ascv1.AgentRunState_AGENT_RUN_STATE_CANCELLED:       {"cancelled", "Stopped", "idle", false},
	ascv1.AgentRunState_AGENT_RUN_STATE_BUDGET_EXCEEDED: {"budget", "Budget reached", "warn", false},
	ascv1.AgentRunState_AGENT_RUN_STATE_EXPIRED:         {"expired", "Timed out", "warn", false},
}

// RunState describes an agent run state. An unknown value is drawn as "Unknown" and treated as finished, so a
// page never polls forever on a state it does not understand.
func RunState(s ascv1.AgentRunState) State {
	if st, ok := runStates[s]; ok {
		return st
	}
	return State{"unknown", "Unknown", "idle", false}
}

// RunStateFilter maps a query-string slug to the states it selects. An unknown slug is refused (ok false); the
// empty slug selects every state.
func RunStateFilter(slug string) ([]ascv1.AgentRunState, bool) {
	switch slug {
	case "":
		return nil, true
	case "active":
		return []ascv1.AgentRunState{
			ascv1.AgentRunState_AGENT_RUN_STATE_QUEUED,
			ascv1.AgentRunState_AGENT_RUN_STATE_RUNNING,
			ascv1.AgentRunState_AGENT_RUN_STATE_WAITING_INPUT,
		}, true
	}
	for k, st := range runStates {
		if st.Slug == slug {
			return []ascv1.AgentRunState{k}, true
		}
	}
	return nil, false
}

// RunStateSlugs lists the filter slugs in the order the filter bar shows them.
var RunStateSlugs = []State{
	{Slug: "active", Label: "Running"},
	{Slug: "waiting", Label: "Waiting"},
	{Slug: "succeeded", Label: "Done"},
	{Slug: "failed", Label: "Failed"},
	{Slug: "cancelled", Label: "Stopped"},
	{Slug: "budget", Label: "Budget reached"},
}

var tokenStates = map[ascv1.AgentTokenState]State{
	ascv1.AgentTokenState_AGENT_TOKEN_STATE_ACTIVE:    {"active", "Active", "ok", false},
	ascv1.AgentTokenState_AGENT_TOKEN_STATE_EXPIRED:   {"expired", "Expired", "idle", false},
	ascv1.AgentTokenState_AGENT_TOKEN_STATE_REVOKED:   {"revoked", "Revoked", "bad", false},
	ascv1.AgentTokenState_AGENT_TOKEN_STATE_EXHAUSTED: {"exhausted", "Budget spent", "warn", false},
}

// TokenState describes an agent token state.
func TokenState(s ascv1.AgentTokenState) State {
	if st, ok := tokenStates[s]; ok {
		return st
	}
	return State{"unknown", "Unknown", "idle", false}
}

var workflowRunStates = map[ascv1.WorkflowRunState]State{
	ascv1.WorkflowRunState_WORKFLOW_RUN_STATE_QUEUED:           {"queued", "Queued", "idle", true},
	ascv1.WorkflowRunState_WORKFLOW_RUN_STATE_RUNNING:          {"running", "Running", "run", true},
	ascv1.WorkflowRunState_WORKFLOW_RUN_STATE_WAITING_APPROVAL: {"waiting", "Waiting for approval", "wait", true},
	ascv1.WorkflowRunState_WORKFLOW_RUN_STATE_SUCCEEDED:        {"succeeded", "Succeeded", "ok", false},
	ascv1.WorkflowRunState_WORKFLOW_RUN_STATE_FAILED:           {"failed", "Failed", "bad", false},
	ascv1.WorkflowRunState_WORKFLOW_RUN_STATE_CANCELLED:        {"cancelled", "Cancelled", "idle", false},
}

// WorkflowRunState describes a workflow run state.
func WorkflowRunState(s ascv1.WorkflowRunState) State {
	if st, ok := workflowRunStates[s]; ok {
		return st
	}
	return State{"unknown", "Unknown", "idle", false}
}

// WorkflowRunStateFilter maps a query-string slug to workflow run states. The empty slug selects all.
func WorkflowRunStateFilter(slug string) ([]ascv1.WorkflowRunState, bool) {
	switch slug {
	case "":
		return nil, true
	case "active":
		return []ascv1.WorkflowRunState{
			ascv1.WorkflowRunState_WORKFLOW_RUN_STATE_QUEUED,
			ascv1.WorkflowRunState_WORKFLOW_RUN_STATE_RUNNING,
			ascv1.WorkflowRunState_WORKFLOW_RUN_STATE_WAITING_APPROVAL,
		}, true
	}
	for k, st := range workflowRunStates {
		if st.Slug == slug {
			return []ascv1.WorkflowRunState{k}, true
		}
	}
	return nil, false
}

var stepStates = map[ascv1.WorkflowStepState]State{
	ascv1.WorkflowStepState_WORKFLOW_STEP_STATE_PENDING:   {"pending", "Pending", "idle", true},
	ascv1.WorkflowStepState_WORKFLOW_STEP_STATE_RUNNING:   {"running", "Running", "run", true},
	ascv1.WorkflowStepState_WORKFLOW_STEP_STATE_WAITING:   {"waiting", "Waiting", "wait", true},
	ascv1.WorkflowStepState_WORKFLOW_STEP_STATE_SUCCEEDED: {"succeeded", "Succeeded", "ok", false},
	ascv1.WorkflowStepState_WORKFLOW_STEP_STATE_FAILED:    {"failed", "Failed", "bad", false},
	ascv1.WorkflowStepState_WORKFLOW_STEP_STATE_SKIPPED:   {"skipped", "Skipped", "idle", false},
	ascv1.WorkflowStepState_WORKFLOW_STEP_STATE_CANCELLED: {"cancelled", "Cancelled", "idle", false},
}

// StepState describes a workflow step state.
func StepState(s ascv1.WorkflowStepState) State {
	if st, ok := stepStates[s]; ok {
		return st
	}
	return State{"unknown", "Unknown", "idle", false}
}

// StepKind names a workflow step kind for people.
func StepKind(k ascv1.WorkflowStepKind) string {
	switch k {
	case ascv1.WorkflowStepKind_WORKFLOW_STEP_KIND_AGENT:
		return "Agent task"
	case ascv1.WorkflowStepKind_WORKFLOW_STEP_KIND_APPROVAL:
		return "Approval"
	case ascv1.WorkflowStepKind_WORKFLOW_STEP_KIND_CHECKS:
		return "Checks"
	case ascv1.WorkflowStepKind_WORKFLOW_STEP_KIND_COMMAND:
		return "Command"
	}
	return "Step"
}

// TriggerLabel names a workflow trigger kind for people.
func TriggerLabel(k ascv1.WorkflowTriggerKind) string {
	switch k {
	case ascv1.WorkflowTriggerKind_WORKFLOW_TRIGGER_KIND_MANUAL:
		return "Started by hand"
	case ascv1.WorkflowTriggerKind_WORKFLOW_TRIGGER_KIND_CHANGE_OPENED:
		return "A change was opened"
	case ascv1.WorkflowTriggerKind_WORKFLOW_TRIGGER_KIND_CHANGE_UPDATED:
		return "A change was updated"
	case ascv1.WorkflowTriggerKind_WORKFLOW_TRIGGER_KIND_CHECK_FAILED:
		return "A check failed"
	case ascv1.WorkflowTriggerKind_WORKFLOW_TRIGGER_KIND_COMMENT_MENTION:
		return "An agent was mentioned"
	case ascv1.WorkflowTriggerKind_WORKFLOW_TRIGGER_KIND_SCHEDULE:
		return "On a schedule"
	case ascv1.WorkflowTriggerKind_WORKFLOW_TRIGGER_KIND_LAND:
		return "A stack landed"
	}
	return "Unknown trigger"
}

// EventKind names an agent run event kind: a short word for the log margin and a tone.
func EventKind(k ascv1.AgentRunEventKind) (word, tone string) {
	switch k {
	case ascv1.AgentRunEventKind_AGENT_RUN_EVENT_KIND_STATE:
		return "State", "idle"
	case ascv1.AgentRunEventKind_AGENT_RUN_EVENT_KIND_MESSAGE:
		return "Agent", "run"
	case ascv1.AgentRunEventKind_AGENT_RUN_EVENT_KIND_INPUT:
		return "You", "wait"
	case ascv1.AgentRunEventKind_AGENT_RUN_EVENT_KIND_TOOL_CALL:
		return "Tool", "idle"
	case ascv1.AgentRunEventKind_AGENT_RUN_EVENT_KIND_TOOL_RESULT:
		return "Result", "ok"
	case ascv1.AgentRunEventKind_AGENT_RUN_EVENT_KIND_CHANGE:
		return "Change", "ok"
	case ascv1.AgentRunEventKind_AGENT_RUN_EVENT_KIND_USAGE:
		return "Usage", "idle"
	case ascv1.AgentRunEventKind_AGENT_RUN_EVENT_KIND_ERROR:
		return "Error", "bad"
	}
	return "Event", "idle"
}

// Time turns unix seconds into a time, or the zero time for 0 or a value outside a sane range.
func Time(sec int64) time.Time {
	if sec <= 0 || sec > 1<<36 {
		return time.Time{}
	}
	return time.Unix(sec, 0).UTC()
}

// TimeMs turns unix milliseconds into a time.
func TimeMs(ms int64) time.Time {
	if ms <= 0 || ms > 1<<46 {
		return time.Time{}
	}
	return time.UnixMilli(ms).UTC()
}

// ChangeText encodes a 16-byte change id as the 32 letter text form (z is the nibble 0, k the nibble 15). It
// returns "" for any other length.
func ChangeText(raw []byte) string {
	if len(raw) != 16 {
		return ""
	}
	var b strings.Builder
	b.Grow(32)
	for _, v := range raw {
		b.WriteByte('z' - v>>4)
		b.WriteByte('z' - v&0x0f)
	}
	return b.String()
}

// ChangeBytes is the inverse of ChangeText. It returns nil for text that is not a change id.
func ChangeBytes(s string) []byte {
	if !issues_model.IsAscChange(s) {
		return nil
	}
	raw := make([]byte, 16)
	for i := range raw {
		raw[i] = (('z' - s[2*i]) << 4) | ('z' - s[2*i+1])
	}
	return raw
}

// ShortChange is the 12 letter form shown to people.
func ShortChange(text string) string {
	if len(text) > 12 {
		return text[:12]
	}
	return text
}
