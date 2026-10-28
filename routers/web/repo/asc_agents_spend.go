// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	"net/http"

	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/base"
	"forgejo.org/modules/log"
	"forgejo.org/services/ascagentui"
	"forgejo.org/services/context"
)

const tplAscAgentsSpend base.TplName = "repo/asc_agents_spend"

// The spend page of the agents area: what agents spent this month, the cap of the repository and of each agent, the
// daily series with a table that says the same. Reading needs code read access (as the run list); setting a cap needs
// repository admin and is made as the person, so asc decides again and records who changed it.

// AscAgentsSpend serves `GET /{owner}/{repo}/asc/agents/spend?month=YYYY-MM`.
func AscAgentsSpend(ctx *context.Context) {
	env, ok := ascAgentsGate(ctx, ascAgentsOpts{level: ascAgentsRead})
	if !ok {
		return
	}
	ctx.Data["Title"] = "Agent spend"
	ascAgentsNav(ctx, "spend")
	month := ctx.FormTrim("month")
	if month != "" && len(month) != 7 {
		month = ""
	}
	conn, err := ascRPCConn()
	if err != nil {
		ascAgentsFail(ctx, tplAscAgentsSpend, "connection", err)
		return
	}
	callCtx, cancel := ascAgentsReadCtx(ctx)
	defer cancel()
	resp, err := ascv1.NewAgentsClient(conn).GetSpend(callCtx, &ascv1.GetAgentSpendRequest{Repo: env.ref(), Month: month})
	if err != nil {
		ascAgentsFail(ctx, tplAscAgentsSpend, "GetSpend", err)
		return
	}
	ctx.Data["AscAgentSpend"] = ascagentui.SpendOf(resp)
	ctx.Data["AscAgentsCanBudget"] = ctx.Repo.IsAdmin() && !ctx.Repo.Repository.IsArchived
	ctx.HTML(http.StatusOK, tplAscAgentsSpend)
}

// AscAgentsSpendBudget serves `POST /{owner}/{repo}/asc/agents/spend/budget`: the monthly cap of the repository (agent
// empty) or of one agent. An empty or 0 cap removes it.
func AscAgentsSpendBudget(ctx *context.Context) {
	env, ok := ascAgentsGate(ctx, ascAgentsOpts{level: ascAgentsAdmin, notArchived: true})
	if !ok {
		return
	}
	ascAgentsLimitBody(ctx)
	back := env.runsLink() + "/spend"
	agent := ctx.FormTrim("agent")
	if agent != "" && !ascagentui.ValidAgentName(agent) {
		ctx.Flash.Error("Choose an agent from the list.")
		ctx.Redirect(back)
		return
	}
	capMicro, ok := ascagentui.ParseUSD(ctx.FormTrim("cap_usd"))
	if !ok {
		ctx.Flash.Error("Enter the cap in dollars, such as 400 or 120.50, or leave it empty to remove it.")
		ctx.Redirect(back)
		return
	}
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
	if _, err := ascv1.NewAgentsClient(conn).SetMonthlyBudget(callCtx, &ascv1.SetMonthlyBudgetRequest{Repo: env.ref(), Agent: agent, CapMicroUsd: capMicro}); err != nil {
		ascAgentsFlashFail(ctx, back, "SetMonthlyBudget", err)
		return
	}
	log.Info("asc agents: monthly budget of repository %d set by user %d", ctx.Repo.Repository.ID, ctx.Doer.ID)
	if capMicro == 0 {
		ctx.Flash.Success("The monthly cap was removed.")
	} else {
		ctx.Flash.Success("The monthly cap was saved. A run that would start over it is refused.")
	}
	ctx.Redirect(back)
}
