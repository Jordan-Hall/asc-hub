// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	gocontext "context"
	"net/http"
	"strconv"

	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/log"
	"forgejo.org/services/ascagentui"
	"forgejo.org/services/context"
)

// The extras of the new-task page: the agents that already work in the repository (with their scope), the estimate from
// similar finished runs, and the recent tasks to reuse. They are read after the gate with the internal credential; none of
// them starts anything.

// ascAgentEstimate is the "About 10 min and $1.60" line.
type ascAgentEstimate struct {
	Samples  uint32 `json:"samples"`
	Duration string `json:"duration,omitempty"`
	Cost     string `json:"cost,omitempty"`
	Known    bool   `json:"known"`
}

func ascAgentEstimateOf(r *ascv1.GetAgentEstimateResponse) ascAgentEstimate {
	if r.GetSamples() == 0 {
		return ascAgentEstimate{}
	}
	return ascAgentEstimate{
		Samples:  min(r.GetSamples(), 50),
		Duration: ascagentui.FormatDuration(uint64(r.GetSeconds())),
		Cost:     ascagentui.FormatUSD(r.GetCostMicroUsd()),
		Known:    true,
	}
}

// ascAgentRecentTask is a task text that can be put into the form again.
type ascAgentRecentTask struct {
	Title string
	Task  string
	Agent string
	RunID uint64
}

func ascAgentsLoadNewExtras(ctx *context.Context, env *ascAgentsEnv, readCtx, userCtx gocontext.Context, form *ascAgentForm) {
	conn, err := ascRPCConn()
	if err != nil {
		return
	}
	api := ascv1.NewAgentsClient(conn)
	if resp, err := api.ListAgents(readCtx, &ascv1.ListRepoAgentsRequest{Repo: env.ref()}); err == nil {
		ctx.Data["AscAgentsRoster"] = ascagentui.RosterOf(resp.GetAgents())
	} else {
		log.Warn("asc agents: ListAgents for the form failed: %s", ascagentuiLogLine(ascagentui.Classify(err)))
	}
	agent := form.Agent
	if !ascagentui.ValidAgentName(agent) {
		agent = ascAgentsDefaultAgent
	}
	if resp, err := api.GetEstimate(readCtx, &ascv1.GetAgentEstimateRequest{Repo: env.ref(), Agent: agent, TaskBytes: uint32(min(len(form.Task), ascagentui.MaxTaskBytes))}); err == nil {
		ctx.Data["AscAgentEstimate"] = ascAgentEstimateOf(resp)
	}
	// The recent tasks are read as the person: asc shows a person their own tasks and an administrator everyone's, so a
	// task text is never shown to someone who did not write it unless they administer the repository.
	if resp, err := api.RecentTasks(userCtx, &ascv1.RecentAgentTasksRequest{Repo: env.ref(), Limit: 5}); err == nil {
		var out []ascAgentRecentTask
		for _, t := range resp.GetTasks() {
			task, ok := ascagentui.CheckFreeText(t.GetTask(), ascagentui.MaxTaskBytes)
			if !ok || t.GetRunId() == 0 {
				continue
			}
			title := ascagentui.CleanLine(t.GetTitle(), ascagentui.MaxTitleBytes)
			if title == "" {
				title = ascagentui.CleanLine(task, 80)
			}
			out = append(out, ascAgentRecentTask{Title: title, Task: ascagentui.CleanText(task, ascagentui.MaxTaskBytes), Agent: ascagentui.SafeName(t.GetAgent()), RunID: t.GetRunId()})
		}
		ctx.Data["AscAgentRecent"] = out
	}
}

// AscAgentsEstimate serves `GET /{owner}/{repo}/asc/agents/estimate?agent=&bytes=` for the form's script.
func AscAgentsEstimate(ctx *context.Context) {
	env, ok := ascAgentsGate(ctx, ascAgentsOpts{level: ascAgentsWrite, notArchived: true})
	if !ok {
		return
	}
	agent := ctx.FormTrim("agent")
	if agent == "" {
		agent = ascAgentsDefaultAgent
	}
	n, err := strconv.ParseUint(ctx.FormString("bytes"), 10, 32)
	if !ascagentui.ValidAgentName(agent) || (ctx.FormString("bytes") != "" && err != nil) {
		ctx.JSON(http.StatusBadRequest, map[string]string{"error": "bad query"})
		return
	}
	ascAgentsNoStore(ctx)
	conn, err := ascRPCConn()
	if err != nil {
		ctx.JSON(http.StatusBadGateway, map[string]string{"error": ascagentui.Classify(err).Message})
		return
	}
	callCtx, cancel := ascAgentsReadCtx(ctx)
	defer cancel()
	resp, err := ascv1.NewAgentsClient(conn).GetEstimate(callCtx, &ascv1.GetAgentEstimateRequest{Repo: env.ref(), Agent: agent, TaskBytes: uint32(min(n, ascagentui.MaxTaskBytes))})
	if err != nil {
		f := ascagentui.Classify(err)
		ctx.JSON(f.HTTP, map[string]string{"error": f.Message})
		return
	}
	ctx.JSON(http.StatusOK, ascAgentEstimateOf(resp))
}
