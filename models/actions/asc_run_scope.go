// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package actions

import (
	"context"
	"strings"

	user_model "forgejo.org/models/user"
	"forgejo.org/modules/ascscope"
	webhook_module "forgejo.org/modules/webhook"
)

// AscScope is the scope of the run, which decides the secrets and variables it receives (ascscope.Scope):
// Agent runs when an agent started it or opened the pull request it runs for; else Releases for a release event
// or a tag; else Checks.
func (run *ActionRun) AscScope(ctx context.Context) (ascscope.Scope, error) {
	if err := run.LoadAttributes(ctx); err != nil {
		return 0, err
	}
	if run.TriggerUser.IsAscAgent() {
		return ascscope.ScopeAgents, nil
	}
	if run.PullRequestPosterID > 0 && run.PullRequestPosterID != run.TriggerUserID {
		poster, err := user_model.GetPossibleUserByID(ctx, run.PullRequestPosterID)
		if err != nil && !user_model.IsErrUserNotExist(err) {
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
