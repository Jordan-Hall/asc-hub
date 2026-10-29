// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	gocontext "context"
	"errors"
	"net/http"
	"strconv"

	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/log"
	"forgejo.org/services/ascagentui"
	"forgejo.org/services/context"

	"google.golang.org/grpc"
)

// Control of one run: pause and resume, limits of a live run, continue a stopped run with higher limits, roll back to a
// checkpoint, download the audit log. Each action needs code write access on an unarchived repository and is made as the
// signed-in person, so asc decides again (who may pause a run, who may roll one back) and records the person.

// ascAgentsRunPlan is the plan of a run as drawn: the steps and what was decided about them.
type ascAgentsRunPlan struct {
	Request   ascagentui.Request
	Pending   bool
	Approved  bool
	Rejected  bool
	DecidedBy string
	DecidedAt string // "10:02", UTC
}

// ascAgentsLoadRunBoard fills what the run page shows besides the run itself: the runs rail, the requests, the plan,
// the checkpoints. A failed read leaves its card out; the page still draws.
func ascAgentsLoadRunBoard(ctx *context.Context, env *ascAgentsEnv, callCtx gocontext.Context, conn grpc.ClientConnInterface, run *ascv1.AgentRun, see bool) {
	api := ascv1.NewAgentsClient(conn)
	id := run.GetId()

	if resp, err := api.ListRuns(callCtx, &ascv1.ListAgentRunsRequest{Repo: env.ref(), Limit: 8}); err == nil {
		var rail []ascAgentRunRow
		for _, r := range resp.GetRuns() {
			if row, ok := ascAgentRunOf(r, env.name, env.runsLink(), 0); ok {
				rail = append(rail, row)
			}
		}
		ctx.Data["AscAgentRunRail"] = rail
	} else {
		log.Warn("asc agents: ListRuns for the rail failed: %s", ascagentuiLogLine(ascagentui.Classify(err)))
	}

	if !see {
		return // the plan, the requests and the checkpoints are private to the creator and the administrators
	}
	if resp, err := api.ListRequests(callCtx, &ascv1.ListAgentRequestsRequest{Repo: env.ref(), RunId: id, Limit: 50}); err == nil {
		reqs := ascagentui.Requests(resp.GetRequests())
		var plan *ascAgentsRunPlan
		for _, r := range reqs {
			if r.RunID != id {
				continue
			}
			switch {
			case r.Kind == "permission" && r.Pending && ctx.Data["AscAgentRequest"] == nil:
				ctx.Data["AscAgentRequest"] = r
			case r.Kind == "plan" && plan == nil:
				p := &ascAgentsRunPlan{Request: r, Pending: r.Pending, Approved: r.State == "approved", Rejected: r.State == "rejected", DecidedBy: r.DecidedBy}
				if !r.DecidedAt.IsZero() {
					p.DecidedAt = r.DecidedAt.UTC().Format("15:04")
				}
				plan = p
			}
		}
		if plan != nil {
			ctx.Data["AscAgentPlan"] = plan
		}
		ctx.Data["AscAgentApprovals"] = reqs
	} else {
		log.Warn("asc agents: ListRequests for a run failed: %s", ascagentuiLogLine(ascagentui.Classify(err)))
		ctx.Data["AscAgentRunPartial"] = true
	}

	if resp, err := api.ListCheckpoints(callCtx, &ascv1.ListAgentCheckpointsRequest{Repo: env.ref(), RunId: id}); err == nil {
		ctx.Data["AscAgentCheckpoints"] = ascagentui.CheckpointsOf(resp.GetCheckpoints(), id)
	} else {
		ctx.Data["AscAgentRunPartial"] = true
	}
}

// ascAgentsRunAction is the frame of a change to one run: gate, run id, one call as the person, flash and back.
func ascAgentsRunAction(ctx *context.Context, what string, call func(callCtx gocontext.Context, api ascv1.AgentsClient, env *ascAgentsEnv, id uint64) (msg, to string, err error)) {
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
	conn, err := ascRPCConn()
	if err != nil {
		ascAgentsFlashFail(ctx, back, "connection", err)
		return
	}
	callCtx, done, ok := ascAgentsWriteCtx(ctx, ctx.FormTrim("op"))
	if !ok {
		return
	}
	defer done()
	msg, to, err := call(callCtx, ascv1.NewAgentsClient(conn), env, id)
	if err != nil {
		var be *errAscAgentsForm
		if errors.As(err, &be) {
			ctx.Flash.Error(be.msg)
			ctx.Redirect(back)
			return
		}
		ascAgentsFlashFail(ctx, back, what, err)
		return
	}
	ctx.Flash.Success(msg)
	if to == "" {
		to = back
	}
	ctx.Redirect(to)
}

// errAscAgentsForm is a refusal of the form itself; nothing was sent to asc.
type errAscAgentsForm struct{ msg string }

func (e *errAscAgentsForm) Error() string { return e.msg }

func ascAgentsBudgetForm(ctx *context.Context) (*ascv1.AgentBudgetLimits, error) {
	limits, err := ascagentui.ParseBudget(ascagentui.BudgetInput{
		Changes: ctx.FormTrim(ascagentui.BudgetFields.Changes),
		Tokens:  ctx.FormTrim(ascagentui.BudgetFields.Tokens),
		CostUSD: ctx.FormTrim(ascagentui.BudgetFields.CostUSD),
		Minutes: ctx.FormTrim(ascagentui.BudgetFields.Minutes),
	})
	if err != nil {
		return nil, &errAscAgentsForm{"A limit is not a whole number (cost: dollars, such as 5 or 1.50)."}
	}
	return limits, nil
}

func ascAgentsLoweredText(prefix string, lowered []string) string {
	var safe []string
	for _, l := range lowered {
		if c := ascagentui.SafeCode(l); c != "" {
			safe = append(safe, c)
		}
	}
	if len(safe) == 0 {
		return prefix
	}
	out := prefix + " Lowered to the policy cap:"
	for i, s := range safe {
		if i > 0 {
			out += ","
		}
		out += " " + s
	}
	return out + "."
}

// AscAgentsPause serves `POST .../runs/{id}/pause` with paused=1 to hold the run at its next tool call, paused=0 to resume.
func AscAgentsPause(ctx *context.Context) {
	pause := ctx.FormString("paused") != "0"
	ascAgentsRunAction(ctx, "PauseRun", func(callCtx gocontext.Context, api ascv1.AgentsClient, env *ascAgentsEnv, id uint64) (string, string, error) {
		reason, ok := ascagentui.CheckLine(ctx.FormString("reason"), ascagentui.MaxReasonBytes, true)
		if !ok {
			return "", "", &errAscAgentsForm{"The reason is at most 256 characters on one line."}
		}
		resp, err := api.PauseRun(callCtx, &ascv1.PauseAgentRunRequest{Repo: env.ref(), Id: id, Paused: pause, Reason: reason})
		if err != nil {
			return "", "", err
		}
		if _, ok := ascAgentRunOf(resp.GetRun(), env.name, env.runsLink(), id); !ok {
			return "", "", errAscAgentsBadAnswer
		}
		if pause {
			return "The run is paused. It holds at its next tool call.", "", nil
		}
		return "The run continues.", "", nil
	})
}

// AscAgentsLimits serves `POST .../runs/{id}/limits`: new limits for a live run. A field left empty keeps its value;
// asc lowers anything above the policy cap and names it.
func AscAgentsLimits(ctx *context.Context) {
	ascAgentsRunAction(ctx, "SetRunLimits", func(callCtx gocontext.Context, api ascv1.AgentsClient, env *ascAgentsEnv, id uint64) (string, string, error) {
		limits, err := ascAgentsBudgetForm(ctx)
		if err != nil {
			return "", "", err
		}
		resp, err := api.SetRunLimits(callCtx, &ascv1.SetAgentRunLimitsRequest{Repo: env.ref(), Id: id, Limits: limits})
		if err != nil {
			return "", "", err
		}
		if _, ok := ascAgentRunOf(resp.GetRun(), env.name, env.runsLink(), id); !ok {
			return "", "", errAscAgentsBadAnswer
		}
		return ascAgentsLoweredText("The limits of the run were changed.", resp.GetLowered()), "", nil
	})
}

// AscAgentsContinue serves `POST .../runs/{id}/continue`: a new run that continues a finished one with raised limits.
func AscAgentsContinue(ctx *context.Context) {
	ascAgentsRunAction(ctx, "ContinueRun", func(callCtx gocontext.Context, api ascv1.AgentsClient, env *ascAgentsEnv, id uint64) (string, string, error) {
		limits, err := ascAgentsBudgetForm(ctx)
		if err != nil {
			return "", "", err
		}
		resp, err := api.ContinueRun(callCtx, &ascv1.ContinueAgentRunRequest{Repo: env.ref(), Id: id, Limits: limits})
		if err != nil {
			return "", "", err
		}
		row, ok := ascAgentRunOf(resp.GetRun(), env.name, env.runsLink(), 0)
		if !ok || row.ID == id {
			return "", "", errAscAgentsBadAnswer
		}
		return ascAgentsLoweredText("The run continues as a new run.", resp.GetLowered()), row.Link, nil
	})
}

// AscAgentsRollback serves `POST .../runs/{id}/rollback`: a paused run goes back to a checkpoint, logged with the person
// and the reason.
func AscAgentsRollback(ctx *context.Context) {
	ascAgentsRunAction(ctx, "RollbackRun", func(callCtx gocontext.Context, api ascv1.AgentsClient, env *ascAgentsEnv, id uint64) (string, string, error) {
		cp, ok := ascAgentsParseID(ctx.FormString("checkpoint"))
		if !ok {
			return "", "", &errAscAgentsForm{"Choose a checkpoint."}
		}
		reason, ok := ascagentui.CheckLine(ctx.FormString("reason"), ascagentui.MaxReasonBytes, true)
		if !ok {
			return "", "", &errAscAgentsForm{"The reason is at most 256 characters on one line."}
		}
		resp, err := api.RollbackRun(callCtx, &ascv1.RollbackAgentRunRequest{Repo: env.ref(), RunId: id, CheckpointId: cp, Reason: reason})
		if err != nil {
			return "", "", err
		}
		if _, ok := ascAgentRunOf(resp.GetRun(), env.name, env.runsLink(), id); !ok || resp.GetCheckpoint().GetRunId() != id {
			return "", "", errAscAgentsBadAnswer
		}
		return "Rolled back. The run restarts from that checkpoint at its next tool call, and the rollback is logged.", "", nil
	})
}

// AscAgentsLog serves `GET .../runs/{id}/log`: the events, decisions and rollbacks of the run as JSON lines. It is
// the same read access as the run page (code read), and the answer is sent as a download, never as a page.
func AscAgentsLog(ctx *context.Context) {
	env, ok := ascAgentsGate(ctx, ascAgentsOpts{level: ascAgentsRead})
	if !ok {
		return
	}
	id, ok := ascAgentsParseID(ctx.Params("id"))
	if !ok {
		ctx.NotFound("asc agent run", nil)
		return
	}
	conn, err := ascRPCConn()
	if err != nil {
		ctx.Error(http.StatusBadGateway)
		return
	}
	callCtx, cancel := ascAgentsReadCtx(ctx)
	defer cancel()
	api := ascv1.NewAgentsClient(conn)
	// The run must belong to this repository before its log is sent.
	got, err := api.GetRun(callCtx, &ascv1.GetAgentRunRequest{Repo: env.ref(), Id: id})
	if err != nil {
		ascAgentsFail(ctx, tplAscAgentsRun, "GetRun", err)
		return
	}
	if _, ok := ascAgentRunOf(got.GetRun(), env.name, env.runsLink(), id); !ok {
		ctx.NotFound("asc agent run", nil)
		return
	}
	if !ascAgentsMaySeeRun(ctx, got.GetRun().GetCreatedBy()) {
		ctx.Error(http.StatusForbidden) // the log holds what the agent read and wrote
		return
	}
	resp, err := api.GetRunLog(callCtx, &ascv1.GetAgentRunLogRequest{Repo: env.ref(), Id: id})
	if err != nil {
		ascAgentsFail(ctx, tplAscAgentsRun, "GetRunLog", err)
		return
	}
	ascAgentsNoStore(ctx)
	h := ctx.Resp.Header()
	h.Set("Content-Type", "application/x-ndjson; charset=utf-8")
	h.Set("Content-Disposition", `attachment; filename="agent-run-`+strconv.FormatUint(id, 10)+`.jsonl"`)
	h.Set("X-Content-Type-Options", "nosniff")
	if resp.GetTruncated() {
		h.Set("X-Asc-Truncated", "1")
	}
	ctx.Resp.WriteHeader(http.StatusOK)
	_, _ = ctx.Resp.Write(resp.GetJsonl())
}
