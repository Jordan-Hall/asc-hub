// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package structs

import "forgejo.org/modules/json"

// AscHookPayload is the body of the ASC events a webhook can send: a change opened or landed, a review requested, an
// agent run started or finished, a release published, the policy changed. Event names it, as "release.published";
// exactly one of Change, Run, Release and Policy is set, after the event.
type AscHookPayload struct {
	Event      string          `json:"event"`
	Repository string          `json:"repository"`
	Change     *AscHookChange  `json:"change,omitempty"`
	Run        *AscHookRun     `json:"run,omitempty"`
	Release    *AscHookRelease `json:"release,omitempty"`
	Policy     *AscHookPolicy  `json:"policy,omitempty"`
	Sender     string          `json:"sender,omitempty"`
	URL        string          `json:"url"`
}

// AscHookChange is the change of a change.* or review.requested event.
type AscHookChange struct {
	Number   int64  `json:"number"`
	Title    string `json:"title"`
	Base     string `json:"base"`
	Head     string `json:"head"`
	Reviewer string `json:"reviewer,omitempty"`
}

// AscHookRun is the agent run of an agent_run.* event. State is how it ended, empty while it runs.
type AscHookRun struct {
	ID      uint64 `json:"id"`
	State   string `json:"state,omitempty"`
	Changes uint64 `json:"changes"`
}

// AscHookRelease is the release of a release.published event: its tag, the branch and commit it was cut from
// ("main@a41f9c2") and how many changes landed since the release before it.
type AscHookRelease struct {
	Tag     string `json:"tag"`
	Base    string `json:"base"`
	Changes int64  `json:"changes"`
}

// AscHookPolicy is the policy change of a policy.changed event: the trunk commits before and after, and what changed.
type AscHookPolicy struct {
	Before  string `json:"before"`
	After   string `json:"after"`
	Summary string `json:"summary"`
}

// JSONPayload implements Payloader.
func (p *AscHookPayload) JSONPayload() ([]byte, error) {
	return json.MarshalIndent(p, "", "  ")
}
