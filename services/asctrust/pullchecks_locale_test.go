// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package asctrust

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// trace is a translation function that answers with the key and the arguments, so a test sees what was asked for.
func trace(key string, args ...any) string {
	if len(args) == 0 {
		return key
	}
	return fmt.Sprint(key, args)
}

func TestPullChecksLocalizeAsksForTheKeysOfTheSentences(t *testing.T) {
	failing := BuildPullChecks(PullChecksIn{
		Statuses: []StatusIn{{Context: "unit", State: "failure"}, {Context: "lint", State: "error"}, {Context: "fmt", State: "success"}},
		Required: []string{"e2e"}, MainStatuses: []StatusIn{{State: "failure"}},
		Causes:      []CauseIn{{Group: GroupRow{ID: 1, JobName: "", TestName: "", StateKey: "open"}, Occurrences: []OccurrenceRow{{Message: "boom"}}}},
		CausesKnown: true,
	})
	failing.Localize(trace)
	assert.Equal(t, "asc_trust.pull.headline_failed_other_one[2 1]", failing.Headline)
	assert.Equal(t, "asc_trust.pull.pill_failing", failing.Pill)
	assert.Equal(t, "asc_trust.pull.main_failing_one[1]", failing.Main)
	var labels []string
	for _, s := range failing.Statuses {
		labels = append(labels, s.Label)
	}
	assert.Contains(t, labels, "asc_trust.pull.status_failed")
	assert.Contains(t, labels, "asc_trust.pull.status_error")
	assert.Contains(t, labels, "asc_trust.pull.status_pass")
	assert.Contains(t, labels, "asc_trust.pull.status_no_result", "a missing result is never shown as passing")
	assert.Equal(t, "asc_trust.pull.cause_fallback_title", failing.Causes[0].Title)

	for name, tc := range map[string]struct {
		in       PullChecksIn
		headline string
		pill     string
		main     string
	}{
		"running": {PullChecksIn{Statuses: []StatusIn{{Context: "a", State: "pending"}}, MainStatuses: []StatusIn{{State: "pending"}}}, "asc_trust.pull.headline_running", "asc_trust.pull.pill_running", "asc_trust.pull.main_pending"},
		"waiting": {PullChecksIn{Statuses: []StatusIn{{Context: "a", State: "success"}}, Required: []string{"b"}, MainStatuses: []StatusIn{}}, "asc_trust.pull.headline_waiting_one[1]", "asc_trust.pull.pill_waiting", "asc_trust.pull.main_none"},
		"none":    {PullChecksIn{}, "asc_trust.pull.headline_none", "asc_trust.pull.pill_none", "asc_trust.pull.main_unknown"},
		"passed":  {PullChecksIn{Statuses: []StatusIn{{Context: "a", State: "success"}}, MainStatuses: []StatusIn{{State: "success"}}}, "asc_trust.pull.headline_passed", "asc_trust.pull.pill_passing", "asc_trust.pull.main_green"},
	} {
		pc := BuildPullChecks(tc.in)
		pc.Localize(trace)
		assert.Equal(t, tc.headline, pc.Headline, name)
		assert.Equal(t, tc.pill, pc.Pill, name)
		assert.Equal(t, tc.main, pc.Main, name)
	}

	// Without Localize the English sentences stay, so a caller with no locale still has text.
	assert.Equal(t, "All checks passed", BuildPullChecks(PullChecksIn{Statuses: []StatusIn{{Context: "a", State: "success"}}}).Headline)
}

func TestPullChecksLocalizeTitlesTheIssueOfAFlakyCard(t *testing.T) {
	pc := BuildPullChecks(PullChecksIn{Statuses: []StatusIn{{Context: "a", State: "success"}}, Causes: []CauseIn{{
		Group: GroupRow{ID: 4, TestName: "refund_timeout", Flaky: true, StateKey: "open"}, Occurrences: []OccurrenceRow{{Message: "x"}},
	}}, CausesKnown: true})
	if assert.Len(t, pc.Flaky, 1) {
		pc.Localize(func(key string, args ...any) string { return strings.ToUpper(key) + fmt.Sprint(args...) })
		assert.Equal(t, "title="+queryEscape("ASC_TRUST.PULL.FLAKY_ISSUE_TITLErefund_timeout"), pc.Flaky[0].IssueQuery)
	}
}
