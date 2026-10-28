// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	gocontext "context"
	"net/http"
	"strconv"
	"strings"

	"forgejo.org/models/unit"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/log"
	"forgejo.org/services/ascagentui"
	"forgejo.org/services/context"

	"google.golang.org/grpc"
)

// The extra data of the agent boards: the stat cards, the "Needs you" panel, the roster and spend, the decisions on
// permission and plan requests. Every read is made after the gate as the internal credential; every decision is made as
// the signed-in person (ascAgentsWriteCtx), so asc decides it again by its own rules and records who decided.
//
// A read that fails leaves its card out and shows a note; the page itself still draws. Nothing is guessed: a figure that
// asc did not send is not drawn.

// ascAgentStats are the four cards above the list.
type ascAgentStats struct {
	Running      uint32
	ActiveAgents int
	Waiting      int
	Permissions  int
	Plans        int
	HasLanded    bool
	Landed       uint32
	LandedPct    int
	HasSpend     bool
	Spend        ascagentui.Spend
	SpendDay     int
	Total        uint32 // runs of the repository, all states
	HasTotal     bool
	MaxRunning   uint32
}

// ascAgentsLoadBoard fills the stat cards, the needs panel and the roster for the runs page.
func ascAgentsLoadBoard(ctx *context.Context, env *ascAgentsEnv, callCtx gocontext.Context, conn grpc.ClientConnInterface, creators map[uint64]string) {
	api := ascv1.NewAgentsClient(conn)
	var st ascAgentStats
	partial := false

	if resp, err := api.ListAgents(callCtx, &ascv1.ListRepoAgentsRequest{Repo: env.ref()}); err != nil {
		log.Warn("asc agents: ListAgents failed: %s", ascagentuiLogLine(ascagentui.Classify(err)))
		partial = true
	} else {
		roster := ascagentui.RosterOf(resp.GetAgents())
		for _, a := range roster {
			if a.Status != "idle" {
				st.ActiveAgents++
			}
			st.Total += a.Runs
		}
		st.HasTotal = true
		st.Running = resp.GetRunning()
		st.MaxRunning = resp.GetMaxConcurrent()
		st.HasLanded = true
		st.Landed = resp.GetLanded_30D()
		st.LandedPct = int(ascagentui.Percent(uint64(resp.GetLanded_30D()), uint64(resp.GetChanges_30D())))
		ctx.Data["AscAgentsRoster"] = roster
	}

	if !ctx.Repo.CanWrite(unit.TypeCode) {
		// asc shows spend to people who can write; a reader sees no spend card, and no note about it.
	} else if resp, err := api.GetSpend(callCtx, &ascv1.GetAgentSpendRequest{Repo: env.ref()}); err != nil {
		log.Warn("asc agents: GetSpend failed: %s", ascagentuiLogLine(ascagentui.Classify(err)))
		partial = true
	} else {
		st.HasSpend = true
		st.Spend = ascagentui.SpendOf(resp)
		st.SpendDay = ascagentui.MonthDayOf(ascAgentsClock())
	}

	if resp, err := api.ListRequests(callCtx, &ascv1.ListAgentRequestsRequest{
		Repo: env.ref(), States: []ascv1.AgentRequestState{ascv1.AgentRequestState_AGENT_REQUEST_STATE_PENDING}, Limit: 50,
	}); err != nil {
		log.Warn("asc agents: ListRequests failed: %s", ascagentuiLogLine(ascagentui.Classify(err)))
		partial = true
	} else {
		needs := ascagentui.Requests(resp.GetRequests())
		st.Permissions, st.Plans = ascagentui.RequestCounts(needs)
		st.Waiting = st.Permissions + st.Plans
		// The text of a request (what the agent wants to run, the plan) is private to the run's creator and the
		// administrators. A run that is not on this page has an unknown creator: only an administrator sees it.
		for i := range needs {
			if !ascAgentsMaySeeRun(ctx, needs[i].RunID, creators[needs[i].RunID]) {
				needs[i].Redact()
			}
		}
		ctx.Data["AscAgentsNeeds"] = needs
	}
	ctx.Data["AscAgentStats"] = st
	ctx.Data["AscAgentsPartial"] = partial
}

// ascAgentsFilterRows keeps the rows whose title, agent, id or task contains q (case-insensitive). It filters the
// loaded page only: asc has no text search over runs.
func ascAgentsFilterRows(rows []ascAgentRunRow, q string) []ascAgentRunRow {
	q = strings.ToLower(strings.TrimSpace(q))
	if q == "" {
		return rows
	}
	out := rows[:0:0]
	for _, r := range rows {
		hay := strings.ToLower(r.Title + "\n" + r.Agent + "\n" + r.Task + "\n" + strconv.FormatUint(r.ID, 10))
		if strings.Contains(hay, q) {
			out = append(out, r)
		}
	}
	return out
}

// ascAgentsDecideJSON is the answer of a decision made from a page script.
type ascAgentsDecideJSON struct {
	OK       bool   `json:"ok"`
	State    string `json:"state,omitempty"`
	RunState string `json:"runState,omitempty"`
	Message  string `json:"message"`
}

func ascAgentsWantsJSON(ctx *context.Context) bool {
	return ctx.Req.Header.Get("X-Asc-Fetch") == "1"
}

// AscAgentsDecide serves `POST /{owner}/{repo}/asc/agents/requests/{id}/decide`: allow once or deny a permission,
// approve or reject a plan. It needs code write access and an unarchived repository; asc decides again as the person.
func AscAgentsDecide(ctx *context.Context) {
	env, ok := ascAgentsGate(ctx, ascAgentsOpts{level: ascAgentsWrite, notArchived: true})
	if !ok {
		return
	}
	id, ok := ascAgentsParseID(ctx.Params("id"))
	if !ok {
		ctx.NotFound("asc agent request", nil)
		return
	}
	ascAgentsLimitBody(ctx)
	back := env.runsLink()
	if run, ok := ascAgentsParseID(ctx.FormString("run")); ok {
		back = env.runsLink() + "/runs/" + strconv.FormatUint(run, 10)
	}
	fail := func(status int, msg string) {
		if ascAgentsWantsJSON(ctx) {
			ctx.JSON(status, ascAgentsDecideJSON{Message: msg})
			return
		}
		ctx.Flash.Error(msg)
		ctx.Redirect(back)
	}
	decision, _, ok := ascagentui.ParseDecision(ctx.FormString("decision"))
	if !ok {
		fail(http.StatusBadRequest, "Choose allow once, deny, approve or reject.")
		return
	}
	reason, ok := ascagentui.CheckLine(ctx.FormString("reason"), 512, true)
	if !ok {
		fail(http.StatusBadRequest, "The reason is at most 512 characters on one line.")
		return
	}
	conn, err := ascRPCConn()
	if err != nil {
		log.Error("asc agents: no connection to asc serve: %v", err)
		fail(http.StatusBadGateway, ascagentui.Classify(err).Message)
		return
	}
	callCtx, done, ok := ascAgentsWriteCtx(ctx, ctx.FormTrim("op"))
	if !ok {
		return
	}
	defer done()
	resp, err := ascv1.NewAgentsClient(conn).DecideRequest(callCtx, &ascv1.DecideAgentRequestRequest{
		Repo: env.ref(), RequestId: id, Decision: decision, Reason: reason,
	})
	if err != nil {
		f := ascagentui.Classify(err)
		if f != nil && f.Unavailable {
			log.Error("asc agents: DecideRequest failed: %s", ascagentuiLogLine(f))
		}
		msg := "The decision could not be recorded."
		status := http.StatusBadGateway
		if f != nil {
			msg, status = f.Message, f.HTTP
		}
		fail(status, msg)
		return
	}
	req, okReq := ascagentui.RequestOf(resp.GetRequest())
	if !okReq || req.ID != id {
		log.Error("asc agents: DecideRequest answered a request that is not the one asked for")
		fail(http.StatusBadGateway, ascagentui.Classify(errAscAgentsBadAnswer).Message)
		return
	}
	runState := ""
	if row, ok := ascAgentRunOf(resp.GetRun(), env.name, env.runsLink(), req.RunID); ok {
		runState = row.State.Slug
	}
	msg := ascAgentsDecisionText(req)
	if ascAgentsWantsJSON(ctx) {
		ctx.JSON(http.StatusOK, ascAgentsDecideJSON{OK: true, State: req.State, RunState: runState, Message: msg})
		return
	}
	ctx.Flash.Success(msg)
	ctx.Redirect(back)
}

func ascAgentsDecisionText(r ascagentui.Request) string {
	switch r.State {
	case "allowed":
		return "Allowed once. The agent continues."
	case "denied":
		return "Denied. The agent is told and carries on without it."
	case "approved":
		return "Plan approved. The run starts."
	case "rejected":
		return "Plan rejected."
	}
	return "The request was already decided: " + r.State + "."
}
