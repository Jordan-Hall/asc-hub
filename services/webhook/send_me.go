// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package webhook

import (
	"strings"

	webhook_model "forgejo.org/models/webhook"
	webhook_module "forgejo.org/modules/webhook"
)

// SendOption is one ASC event of the "Send me" groups of a hook's Settings tab: its name, the locale key of its
// label and whether the hook sends it.
type SendOption struct {
	Event webhook_module.HookEventType
	Label string
	On    bool
}

// SendGroup is one group of the "Send me" choices, under the locale key of its heading.
type SendGroup struct {
	Label   string
	Options []SendOption
}

// SendGroups are the ASC events of the hook w as the Settings tab shows them: Changes, Agents, Releases and Policy.
func SendGroups(w *webhook_model.Webhook) []SendGroup {
	opt := func(e webhook_module.HookEventType, label string) SendOption {
		return SendOption{Event: e, Label: "asc_hooks." + label, On: w.HasAscEvent(e)}
	}
	changes := SendGroup{"asc_hooks.group_changes", []SendOption{
		opt(webhook_module.HookEventChangeLanded, "opt_landed"),
		opt(webhook_module.HookEventChangeOpened, "opt_opened"),
		opt(webhook_module.HookEventReviewRequested, "opt_review_requested"),
	}}
	agents := SendGroup{"asc_hooks.group_agents", []SendOption{
		opt(webhook_module.HookEventAgentRunStarted, "opt_run_started"),
		opt(webhook_module.HookEventAgentRunFinished, "opt_run_finished"),
	}}
	releases := SendGroup{"asc_hooks.group_releases", []SendOption{opt(webhook_module.HookEventReleasePublished, "opt_published")}}
	policy := SendGroup{"asc_hooks.group_policy", []SendOption{opt(webhook_module.HookEventPolicyChanged, "opt_changed")}}
	if w.Type == webhook_module.ASCAGENT {
		// an agent hook does not start a run for an agent run (asc_agent.go)
		return []SendGroup{changes, releases, policy}
	}
	return []SendGroup{changes, agents, releases, policy}
}

// AgentHookEvents keeps the ASC events an agent hook may take: every one except the agent run events.
func AgentHookEvents(events []string) []string {
	kept := make([]string, 0, len(events))
	for _, e := range events {
		if !strings.HasPrefix(e, "agent_run.") {
			kept = append(kept, e)
		}
	}
	return kept
}
