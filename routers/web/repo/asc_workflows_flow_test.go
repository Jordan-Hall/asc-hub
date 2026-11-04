// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	"fmt"
	"html/template"
	"testing"

	"forgejo.org/modules/translation"
	"forgejo.org/services/ascagentui"

	"github.com/stretchr/testify/assert"
)

// flowLocale words the flow messages in English with their arguments, which the mock locale leaves out.
type flowLocale struct{ translation.MockLocale }

var flowMessages = map[string]string{
	"asc_review.wf_flow_not_started":      "Not started",
	"asc_review.wf_flow_running_for":      "Running for %s",
	"asc_review.wf_flow_waiting_decision": "Waiting for a decision",
	"asc_review.wf_flow_waiting_for":      "Waiting for %s",
	"asc_review.wf_flow_role":             "anyone with the %s role",
	"asc_review.wf_flow_or":               "%s or %s",
	"asc_review.wf_flow_approved_by":      "Approved by %s",
	"asc_review.wf_flow_rejected_by":      "Rejected by %s",
	"asc_review.wf_flow_agent_done_one":   "%d change proposed, %s",
	"asc_review.wf_flow_agent_done_other": "%d changes proposed, %s",
	"asc_review.wf_flow_passed_in":        "Passed in %s",
	"asc_review.wf_flow_failed_after":     "Failed after %s",
	"asc_review.step_state_succeeded":     "Succeeded",
}

func (flowLocale) TrString(key string, args ...any) string {
	return fmt.Sprintf(flowMessages[key], args...)
}

func (l flowLocale) TrN(cnt any, key1, keyN string, args ...any) template.HTML {
	if n, ok := cnt.(int64); ok && n == 1 {
		return template.HTML(l.TrString(key1, args...))
	}
	return template.HTML(l.TrString(keyN, args...))
}

func TestAscWorkflowStepSub(t *testing.T) {
	state := func(slug string) ascagentui.State { return ascagentui.State{Slug: slug} }
	cases := []struct {
		name string
		step ascWorkflowStepRun
		want string
	}{
		{"pending", ascWorkflowStepRun{State: state("pending")}, "Not started"},
		{"running", ascWorkflowStepRun{State: state("running"), Took: "12 s"}, "Running for 12 s"},
		{"running, not started yet", ascWorkflowStepRun{State: state("running")}, ""},
		{"an approval without approvers", ascWorkflowStepRun{State: state("waiting"), Waiting: true}, "Waiting for a decision"},
		{"an approval names who decides", ascWorkflowStepRun{State: state("waiting"), Waiting: true, Approvers: []ascWorkflowApprover{{Name: "alice"}, {Name: "bob"}, {Name: "maintainer", Role: true}}}, "Waiting for alice or bob or anyone with the maintainer role"},
		{"an agent that waits", ascWorkflowStepRun{State: state("waiting"), AgentRunID: 7}, ""},
		{"approved", ascWorkflowStepRun{State: state("succeeded"), Kind: "approval", DecidedBy: "alice", Took: "3 min"}, "Approved by alice"},
		{"rejected", ascWorkflowStepRun{State: state("failed"), Kind: "approval", DecidedBy: "bob"}, "Rejected by bob"},
		{"an agent that proposed one change", ascWorkflowStepRun{State: state("succeeded"), Kind: "agent", HasUsage: true, Changes: 1, Cost: "$0.40"}, "1 change proposed, $0.40"},
		{"an agent that proposed changes", ascWorkflowStepRun{State: state("succeeded"), Kind: "agent", HasUsage: true, Changes: 3, Cost: "$1.12"}, "3 changes proposed, $1.12"},
		{"an agent that proposed nothing", ascWorkflowStepRun{State: state("succeeded"), Kind: "agent", HasUsage: true, Cost: "$0.10", Took: "48 s"}, "Passed in 48 s"},
		{"a command passed", ascWorkflowStepRun{State: state("succeeded"), Kind: "command", Took: "48 s"}, "Passed in 48 s"},
		{"a check failed", ascWorkflowStepRun{State: state("failed"), Kind: "checks", Took: "2 min"}, "Failed after 2 min"},
		{"skipped", ascWorkflowStepRun{State: state("skipped")}, ""},
		{"cancelled", ascWorkflowStepRun{State: state("cancelled"), Took: "5 s"}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, ascWorkflowStepSub(flowLocale{}, &tc.step))
		})
	}
}

func TestAscWorkflowApproversOf(t *testing.T) {
	got := ascWorkflowApproversOf([]string{"user:alice", "role:maintainer", "team:core", "user:", "alice", "user:<b>x</b>", "role:a b"})
	assert.Equal(t, []ascWorkflowApprover{{Name: "alice"}, {Name: "maintainer", Role: true}}, got)

	many := make([]string, 40)
	for i := range many {
		many[i] = fmt.Sprintf("user:u%d", i)
	}
	assert.Len(t, ascWorkflowApproversOf(many), ascWorkflowsMaxApprovers)
}

func TestAscWorkflowStepGood(t *testing.T) {
	for slug, good := range map[string]bool{"succeeded": true, "skipped": true, "failed": false, "cancelled": false, "running": false, "pending": false, "waiting": false} {
		assert.Equal(t, good, ascWorkflowStepGood(ascagentui.State{Slug: slug}), slug)
	}
}
