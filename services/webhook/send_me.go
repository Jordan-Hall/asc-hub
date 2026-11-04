// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package webhook

import (
	"slices"
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
	code := SendGroup{"asc_hooks.group_code", []SendOption{{Event: webhook_module.HookEventPush, Label: "asc_hooks.opt_pushes", On: w.HasPushEvent()}}}
	switch w.Type {
	case webhook_module.ASCAGENT:
		// an agent hook does not start a run for an agent run (asc_agent.go)
		return []SendGroup{changes, releases, policy}
	case webhook_module.SOURCEHUT_BUILDS:
		// a build is submitted for a push only (sourcehut/builds.go)
		return []SendGroup{code}
	}
	return []SendGroup{changes, agents, releases, policy, code}
}

// ChooseSent makes w send the events ticked in names that the groups of SendGroups(w) offer it: the ASC events and,
// for a hook that is not an agent hook, pushes. A name the hook is not offered is left out.
func ChooseSent(w *webhook_model.Webhook, names []string) {
	var offered []string
	for _, g := range SendGroups(w) {
		for _, o := range g.Options {
			if slices.Contains(names, string(o.Event)) {
				offered = append(offered, string(o.Event))
			}
		}
	}
	w.ChooseAscEvents(offered)
	if w.Type != webhook_module.ASCAGENT {
		w.Push = slices.Contains(offered, string(webhook_module.HookEventPush))
	}
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
