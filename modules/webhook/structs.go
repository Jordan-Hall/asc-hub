// Copyright 2022 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package webhook

import "slices"

// HookEvents is a set of web hook events
// update TestCreateWebhook in models/webhook/webhook_test.go when adding or changing values here
type HookEvents struct {
	Create                   bool `json:"create"`
	Delete                   bool `json:"delete"`
	Fork                     bool `json:"fork"`
	Issues                   bool `json:"issues"`
	IssueAssign              bool `json:"issue_assign"`
	IssueLabel               bool `json:"issue_label"`
	IssueMilestone           bool `json:"issue_milestone"`
	IssueComment             bool `json:"issue_comment"`
	Push                     bool `json:"push"`
	PullRequest              bool `json:"pull_request"`
	PullRequestAssign        bool `json:"pull_request_assign"`
	PullRequestLabel         bool `json:"pull_request_label"`
	PullRequestMilestone     bool `json:"pull_request_milestone"`
	PullRequestComment       bool `json:"pull_request_comment"`
	PullRequestReview        bool `json:"pull_request_review"`
	PullRequestSync          bool `json:"pull_request_sync"`
	PullRequestReviewRequest bool `json:"pull_request_review_request"`
	Wiki                     bool `json:"wiki"`
	Repository               bool `json:"repository"`
	Release                  bool `json:"release"`
	Package                  bool `json:"package"`
	ActionRunFailure         bool `json:"action_run_failure"`
	ActionRunRecover         bool `json:"action_run_recover"`
	ActionRunSuccess         bool `json:"action_run_success"`

	ChangeOpened     bool `json:"change_opened"`
	ChangeLanded     bool `json:"change_landed"`
	ReviewRequested  bool `json:"review_requested"`
	AgentRunStarted  bool `json:"agent_run_started"`
	AgentRunFinished bool `json:"agent_run_finished"`
	ReleasePublished bool `json:"release_published"`
	PolicyChanged    bool `json:"policy_changed"`
}

// HookEvent represents events that will deliver a hook.
type HookEvent struct {
	PushOnly       bool   `json:"push_only"`
	SendEverything bool   `json:"send_everything"`
	ChooseEvents   bool   `json:"choose_events"`
	BranchFilter   string `json:"branch_filter"`

	HookEvents `json:"events"`
}

// AscEvent reports whether the set has the ASC event e.
func (e *HookEvents) AscEvent(ev HookEventType) bool {
	if f := e.ascField(ev); f != nil {
		return *f
	}
	return false
}

// SetAscEvent turns the ASC event e on or off.
func (e *HookEvents) SetAscEvent(ev HookEventType, on bool) {
	if f := e.ascField(ev); f != nil {
		*f = on
	}
}

// SetAscEvents turns on the ASC events named in names (as "change.landed") and off the others.
func (e *HookEvents) SetAscEvents(names []string) {
	for _, ev := range AscHookEvents {
		e.SetAscEvent(ev, slices.Contains(names, string(ev)))
	}
}

func (e *HookEvents) ascField(ev HookEventType) *bool {
	switch ev {
	case HookEventChangeOpened:
		return &e.ChangeOpened
	case HookEventChangeLanded:
		return &e.ChangeLanded
	case HookEventReviewRequested:
		return &e.ReviewRequested
	case HookEventAgentRunStarted:
		return &e.AgentRunStarted
	case HookEventAgentRunFinished:
		return &e.AgentRunFinished
	case HookEventReleasePublished:
		return &e.ReleasePublished
	case HookEventPolicyChanged:
		return &e.PolicyChanged
	}
	return nil
}
