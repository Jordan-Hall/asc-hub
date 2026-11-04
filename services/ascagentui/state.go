// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package ascagentui

import (
	"strings"
	"time"

	issues_model "forgejo.org/models/issues"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/translation"
)

// State is how a run, a token or a step is drawn: a short label and a tone. The tone picks the colour class
// (`asc-tone-<tone>`); the label is always shown too, so colour is never the only signal.
type State struct {
	Slug   string // the lower-case name used in query strings and class names
	Key    string // the locale key of the label
	Tone   string // ok, run, wait, bad, warn or idle
	Active bool   // the run still changes: poll it
}

var runStates = map[ascv1.AgentRunState]State{
	ascv1.AgentRunState_AGENT_RUN_STATE_QUEUED:          {"queued", "asc_review.run_state_queued", "idle", true},
	ascv1.AgentRunState_AGENT_RUN_STATE_RUNNING:         {"running", "asc_review.run_state_running", "run", true},
	ascv1.AgentRunState_AGENT_RUN_STATE_WAITING_INPUT:   {"waiting", "asc_review.run_state_waiting", "wait", true},
	ascv1.AgentRunState_AGENT_RUN_STATE_SUCCEEDED:       {"succeeded", "asc_review.run_state_succeeded", "ok", false},
	ascv1.AgentRunState_AGENT_RUN_STATE_FAILED:          {"failed", "asc_review.run_state_failed", "bad", false},
	ascv1.AgentRunState_AGENT_RUN_STATE_CANCELLED:       {"cancelled", "asc_review.run_state_cancelled", "idle", false},
	ascv1.AgentRunState_AGENT_RUN_STATE_BUDGET_EXCEEDED: {"budget", "asc_review.run_state_budget", "warn", false},
	ascv1.AgentRunState_AGENT_RUN_STATE_EXPIRED:         {"expired", "asc_review.run_state_expired", "warn", false},
}

// Text is the label of s in the language of l.
func (s State) Text(l translation.Locale) string {
	return l.TrString(s.Key)
}

// RunState describes an agent run state. An unknown value is drawn as "Unknown" and treated as finished, so a
// page never polls forever on a state it does not understand.
func RunState(s ascv1.AgentRunState) State {
	if st, ok := runStates[s]; ok {
		return st
	}
	return State{"unknown", "asc_review.run_state_unknown", "idle", false}
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
	{Slug: "active", Key: "asc_agentui.filter.active"},
	{Slug: "waiting", Key: "asc_agentui.filter.waiting"},
	{Slug: "succeeded", Key: "asc_agentui.filter.succeeded"},
	{Slug: "failed", Key: "asc_agentui.filter.failed"},
	{Slug: "cancelled", Key: "asc_agentui.filter.cancelled"},
	{Slug: "budget", Key: "asc_agentui.filter.budget"},
}

var tokenStates = map[ascv1.AgentTokenState]State{
	ascv1.AgentTokenState_AGENT_TOKEN_STATE_ACTIVE:    {"active", "asc_review.token_state_active", "ok", false},
	ascv1.AgentTokenState_AGENT_TOKEN_STATE_EXPIRED:   {"expired", "asc_review.token_state_expired", "idle", false},
	ascv1.AgentTokenState_AGENT_TOKEN_STATE_REVOKED:   {"revoked", "asc_review.token_state_revoked", "bad", false},
	ascv1.AgentTokenState_AGENT_TOKEN_STATE_EXHAUSTED: {"exhausted", "asc_review.token_state_exhausted", "warn", false},
}

// TokenState describes an agent token state.
func TokenState(s ascv1.AgentTokenState) State {
	if st, ok := tokenStates[s]; ok {
		return st
	}
	return State{"unknown", "asc_review.token_state_unknown", "idle", false}
}

var workflowRunStates = map[ascv1.WorkflowRunState]State{
	ascv1.WorkflowRunState_WORKFLOW_RUN_STATE_QUEUED:           {"queued", "asc_review.wf_state_queued", "idle", true},
	ascv1.WorkflowRunState_WORKFLOW_RUN_STATE_RUNNING:          {"running", "asc_review.wf_state_running", "run", true},
	ascv1.WorkflowRunState_WORKFLOW_RUN_STATE_WAITING_APPROVAL: {"waiting", "asc_review.wf_state_waiting", "wait", true},
	ascv1.WorkflowRunState_WORKFLOW_RUN_STATE_SUCCEEDED:        {"succeeded", "asc_review.wf_state_succeeded", "ok", false},
	ascv1.WorkflowRunState_WORKFLOW_RUN_STATE_FAILED:           {"failed", "asc_review.wf_state_failed", "bad", false},
	ascv1.WorkflowRunState_WORKFLOW_RUN_STATE_CANCELLED:        {"cancelled", "asc_review.wf_state_cancelled", "idle", false},
}

// WorkflowRunState describes a workflow run state.
func WorkflowRunState(s ascv1.WorkflowRunState) State {
	if st, ok := workflowRunStates[s]; ok {
		return st
	}
	return State{"unknown", "asc_review.wf_state_unknown", "idle", false}
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
	ascv1.WorkflowStepState_WORKFLOW_STEP_STATE_PENDING:   {"pending", "asc_review.step_state_pending", "idle", true},
	ascv1.WorkflowStepState_WORKFLOW_STEP_STATE_RUNNING:   {"running", "asc_review.step_state_running", "run", true},
	ascv1.WorkflowStepState_WORKFLOW_STEP_STATE_WAITING:   {"waiting", "asc_review.step_state_waiting", "wait", true},
	ascv1.WorkflowStepState_WORKFLOW_STEP_STATE_SUCCEEDED: {"succeeded", "asc_review.step_state_succeeded", "ok", false},
	ascv1.WorkflowStepState_WORKFLOW_STEP_STATE_FAILED:    {"failed", "asc_review.step_state_failed", "bad", false},
	ascv1.WorkflowStepState_WORKFLOW_STEP_STATE_SKIPPED:   {"skipped", "asc_review.step_state_skipped", "idle", false},
	ascv1.WorkflowStepState_WORKFLOW_STEP_STATE_CANCELLED: {"cancelled", "asc_review.step_state_cancelled", "idle", false},
}

// StepState describes a workflow step state.
func StepState(s ascv1.WorkflowStepState) State {
	if st, ok := stepStates[s]; ok {
		return st
	}
	return State{"unknown", "asc_review.step_state_unknown", "idle", false}
}

// StepKind is the locale key of the name of a workflow step kind.
func StepKind(k ascv1.WorkflowStepKind) string {
	switch k {
	case ascv1.WorkflowStepKind_WORKFLOW_STEP_KIND_AGENT:
		return "asc_agentui.step_kind.agent"
	case ascv1.WorkflowStepKind_WORKFLOW_STEP_KIND_APPROVAL:
		return "asc_agentui.step_kind.approval"
	case ascv1.WorkflowStepKind_WORKFLOW_STEP_KIND_CHECKS:
		return "asc_agentui.step_kind.checks"
	case ascv1.WorkflowStepKind_WORKFLOW_STEP_KIND_COMMAND:
		return "asc_agentui.step_kind.command"
	}
	return "asc_agentui.step_kind.other"
}

// TriggerLabel is the locale key of the name of a workflow trigger kind.
func TriggerLabel(k ascv1.WorkflowTriggerKind) string {
	switch k {
	case ascv1.WorkflowTriggerKind_WORKFLOW_TRIGGER_KIND_MANUAL:
		return "asc_agentui.trigger.manual"
	case ascv1.WorkflowTriggerKind_WORKFLOW_TRIGGER_KIND_CHANGE_OPENED:
		return "asc_agentui.trigger.change_opened"
	case ascv1.WorkflowTriggerKind_WORKFLOW_TRIGGER_KIND_CHANGE_UPDATED:
		return "asc_agentui.trigger.change_updated"
	case ascv1.WorkflowTriggerKind_WORKFLOW_TRIGGER_KIND_CHECK_FAILED:
		return "asc_agentui.trigger.check_failed"
	case ascv1.WorkflowTriggerKind_WORKFLOW_TRIGGER_KIND_COMMENT_MENTION:
		return "asc_agentui.trigger.mention"
	case ascv1.WorkflowTriggerKind_WORKFLOW_TRIGGER_KIND_SCHEDULE:
		return "asc_agentui.trigger.schedule"
	case ascv1.WorkflowTriggerKind_WORKFLOW_TRIGGER_KIND_LAND:
		return "asc_agentui.trigger.land"
	}
	return "asc_agentui.trigger.unknown"
}

// EventKind names an agent run event kind: the locale key of a short word for the log margin, and a tone.
func EventKind(k ascv1.AgentRunEventKind) (word, tone string) {
	switch k {
	case ascv1.AgentRunEventKind_AGENT_RUN_EVENT_KIND_STATE:
		return "asc_agentui.event.state", "idle"
	case ascv1.AgentRunEventKind_AGENT_RUN_EVENT_KIND_MESSAGE:
		return "asc_agentui.event.agent", "run"
	case ascv1.AgentRunEventKind_AGENT_RUN_EVENT_KIND_INPUT:
		return "asc_agentui.event.you", "wait"
	case ascv1.AgentRunEventKind_AGENT_RUN_EVENT_KIND_TOOL_CALL:
		return "asc_agentui.event.tool", "idle"
	case ascv1.AgentRunEventKind_AGENT_RUN_EVENT_KIND_TOOL_RESULT:
		return "asc_agentui.event.result", "ok"
	case ascv1.AgentRunEventKind_AGENT_RUN_EVENT_KIND_CHANGE:
		return "asc_agentui.event.change", "ok"
	case ascv1.AgentRunEventKind_AGENT_RUN_EVENT_KIND_USAGE:
		return "asc_agentui.event.usage", "idle"
	case ascv1.AgentRunEventKind_AGENT_RUN_EVENT_KIND_ERROR:
		return "asc_agentui.event.error", "bad"
	}
	return "asc_agentui.event.event", "idle"
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
