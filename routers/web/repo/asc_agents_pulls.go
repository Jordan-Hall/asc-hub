// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	"errors"
	"strconv"

	"forgejo.org/models/unit"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/log"
	"forgejo.org/services/ascagentui"
	"forgejo.org/services/context"
)

// ascAgentsPullSeams are the parts of opening a run for review that need asc or the git view. Tests replace them.
var ascAgentsPullSeams = ascagentui.DefaultRunPullSeams

// ascAgentsCanOpenPulls reports whether the run page offers "Open for review": the person may write, the repository
// takes pull requests, and a change of the run has no pull request (and is not landed) yet.
func ascAgentsCanOpenPulls(ctx *context.Context, changes []ascagentui.ChangeLink) bool {
	if !ctx.Repo.CanWrite(unit.TypeCode) || ctx.Repo.Repository.IsArchived || !ctx.Repo.Repository.AllowsPulls(ctx) {
		return false
	}
	for _, c := range changes {
		if !c.HasPull {
			return true
		}
	}
	return false
}

// AscAgentsOpenPulls serves `POST /{owner}/{repo}/asc/agents/runs/{id}/pulls`: every change of the run that has no
// pull request gets one, as the agent's own account, bottom to top (services/ascagentui/runpulls.go).
func AscAgentsOpenPulls(ctx *context.Context) {
	env, ok := ascAgentsGate(ctx, ascAgentsOpts{level: ascAgentsWrite, notArchived: true})
	if !ok {
		return
	}
	id, ok := ascAgentsParseID(ctx.Params("id"))
	if !ok {
		ctx.NotFound("asc agent run", nil)
		return
	}
	ascAgentsLimitBody(ctx)
	back := env.runsLink() + "/runs/" + strconv.FormatUint(id, 10)
	if !ctx.Repo.Repository.AllowsPulls(ctx) {
		ctx.Flash.Error(ctx.Tr("asc_review.run_open_review_nopulls"))
		ctx.Redirect(back)
		return
	}
	conn, err := ascRPCConn()
	if err != nil {
		ascAgentsFlashFail(ctx, back, "connection", err)
		return
	}
	callCtx, cancel := ascAgentsReadCtx(ctx)
	defer cancel()
	resp, err := ascv1.NewAgentsClient(conn).GetRun(callCtx, &ascv1.GetAgentRunRequest{Repo: env.ref(), Id: id})
	if err != nil {
		ascAgentsFlashFail(ctx, back, "GetRun", err)
		return
	}
	run := resp.GetRun()
	row, ok := ascAgentRunOf(run, env.name, env.runsLink(), id)
	if !ok {
		ascAgentsFlashFail(ctx, back, "GetRun", errAscAgentsBadAnswer)
		return
	}
	// The pull requests are opened under the hub's own credential: the person's right to write was checked by the gate,
	// and what the agent was allowed to write was decided by asc when the run made its changes.
	results, err := ascagentui.OpenRunPulls(ctx, conn, ctx.Repo.Repository, ascagentui.RunPullRun{
		ID:        id,
		Agent:     row.Agent,
		Title:     row.Title,
		CreatedBy: run.GetCreatedBy(),
		Changes:   run.GetChanges(),
		RunURL:    back,
	}, ascAgentsPullSeams)
	if err != nil {
		if errors.Is(err, ascagentui.ErrAgentNameTaken) {
			ctx.Flash.Error(ctx.Tr("asc_review.run_open_review_taken"))
		} else {
			log.Error("asc agents: opening run %d for review: %v", id, err)
			ctx.Flash.Error(ctx.Tr("asc_review.run_open_review_failed", len(run.GetChanges())))
		}
		ctx.Redirect(back)
		return
	}
	var opened, linked, waiting, failed int
	for _, r := range results {
		switch r.State {
		case ascagentui.RunPullOpened:
			opened++
		case ascagentui.RunPullLinked, ascagentui.RunPullLanded:
			linked++
		case ascagentui.RunPullWaiting:
			waiting++
		default:
			failed++
		}
	}
	switch {
	case failed > 0:
		ctx.Flash.Error(ctx.Tr("asc_review.run_open_review_failed", failed))
	case waiting > 0:
		ctx.Flash.Warning(ctx.Tr("asc_review.run_open_review_waiting", waiting))
	case opened == 0:
		ctx.Flash.Info(ctx.Tr("asc_review.run_open_review_none"))
	default:
		ctx.Flash.Success(ctx.Tr("asc_review.run_open_review_done", opened, linked))
	}
	ctx.Redirect(back)
}
