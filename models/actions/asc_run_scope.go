// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package actions

import (
	"context"
	"strings"

	user_model "forgejo.org/models/user"
	"forgejo.org/modules/ascscope"
	"forgejo.org/modules/json"
	webhook_module "forgejo.org/modules/webhook"
)

// AscScope is the scope of the run, which decides the secrets and variables it receives (ascscope.Scope):
// Agent runs when an agent started it or opened the pull request it runs for; else Releases for a release event
// or a tag; else Checks.
//
// The pull request is the one the run records, or, for an event that runs the default branch's workflow
// (pull_request_target, a comment or review on a pull request), the one its payload names, since such a workflow
// can check out the pull request's code. A run whose starter or pull request author no longer exists has no scope
// (0) and receives nothing, because nobody can tell whether an agent was behind it.
//
// A scheduled run, and a push an agent did not make, is a check: the default branch holds only what landed
// through review, so its workflows are not an agent's code.
func (run *ActionRun) AscScope(ctx context.Context) (ascscope.Scope, error) {
	if err := run.LoadAttributes(ctx); err != nil {
		return 0, err
	}
	if run.TriggerUserID > 0 && run.TriggerUser.ID != run.TriggerUserID {
		return 0, nil
	}
	if run.TriggerUser.IsAscAgent() {
		return ascscope.ScopeAgents, nil
	}
	posterID := run.PullRequestPosterID
	if posterID == 0 {
		posterID = pullRequestPosterOfPayload(run.EventPayload)
	}
	if posterID > 0 && posterID != run.TriggerUserID {
		poster, err := user_model.GetPossibleUserByID(ctx, posterID)
		if user_model.IsErrUserNotExist(err) {
			return 0, nil
		} else if err != nil {
			return 0, err
		}
		if poster.IsAscAgent() {
			return ascscope.ScopeAgents, nil
		}
	}
	if run.Event == webhook_module.HookEventRelease || strings.HasPrefix(run.Ref, "refs/tags/") {
		return ascscope.ScopeReleases, nil
	}
	return ascscope.ScopeChecks, nil
}

// pullRequestPosterOfPayload is the author of the pull request an event payload is about: its pull_request, or
// its issue when that issue is a pull request; 0 when it is about none.
func pullRequestPosterOfPayload(payload string) int64 {
	if payload == "" {
		return 0
	}
	type poster struct {
		ID int64 `json:"id"`
	}
	var p struct {
		PullRequest *struct {
			Poster *poster `json:"user"`
		} `json:"pull_request"`
		Issue *struct {
			Poster      *poster `json:"user"`
			PullRequest any     `json:"pull_request"`
		} `json:"issue"`
	}
	if json.Unmarshal([]byte(payload), &p) != nil {
		return 0
	}
	switch {
	case p.PullRequest != nil && p.PullRequest.Poster != nil:
		return p.PullRequest.Poster.ID
	case p.Issue != nil && p.Issue.PullRequest != nil && p.Issue.Poster != nil:
		return p.Issue.Poster.ID
	}
	return 0
}
