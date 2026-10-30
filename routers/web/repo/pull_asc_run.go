// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	"fmt"

	issues_model "forgejo.org/models/issues"
	"forgejo.org/services/context"
)

// AscRunPull is the line of a pull request that the hub opened for the change of an agent run: who made it, and the
// page of the run. It is drawn at the top of the stacked change (templates/repo/pulls/asc_badges.tmpl).
type AscRunPull struct {
	RunID     uint64
	Agent     string
	StartedBy string
	// URL is the run page; empty when the viewer may not read runs (the agents area is off for them), and the line then
	// says who made the change without a link.
	URL string
}

// prepareAscRunPull puts the run a pull request was opened for into ctx.Data["AscRunPull"]. It never fails the page.
func prepareAscRunPull(ctx *context.Context, pull *issues_model.PullRequest) {
	if pull == nil {
		return
	}
	link, err := issues_model.GetPullRequestAscRun(ctx, pull.ID)
	if err != nil || link == nil {
		return
	}
	out := &AscRunPull{RunID: link.RunID, Agent: link.Agent, StartedBy: link.StartedBy}
	if ascAgentsCanReadRuns(ctx) {
		out.URL = fmt.Sprintf("%s/asc/agents/runs/%d", ctx.Repo.RepoLink, link.RunID)
	}
	ctx.Data["AscRunPull"] = out
}
