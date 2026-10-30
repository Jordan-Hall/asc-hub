// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package admin

import (
	"net/http"

	"forgejo.org/modules/base"
	"forgejo.org/modules/log"
	"forgejo.org/modules/setting"
	"forgejo.org/routers/web/shared/trustweb"
	"forgejo.org/services/asctrust"
	"forgejo.org/services/context"
)

// The agent switch and the default budget of the whole server (docs/BUILDOUT.md section 28): /admin/asc/agents.
// Site administrators only. An organization can only tighten what is set here.
const tplAscAgents base.TplName = "admin/asc_agents"

type ascInstanceAgentsPage struct {
	Base        string
	Settings    *asctrust.InstanceAgents
	Problem     string
	Unsupported bool
	Op          string
}

func ascAgentsGuard(ctx *context.Context) bool {
	if !setting.Asc.Enabled {
		ctx.NotFound("asc agents", nil)
		return false
	}
	if !ctx.IsSigned || ctx.Doer == nil || !ctx.Doer.IsAdmin {
		ctx.Error(http.StatusForbidden)
		return false
	}
	return true
}

// AscAgents serves GET /admin/asc/agents.
func AscAgents(ctx *context.Context) {
	if !ascAgentsGuard(ctx) {
		return
	}
	trustweb.NoStore(ctx)
	p := &ascInstanceAgentsPage{Base: setting.AppSubURL + "/admin/asc/agents", Op: asctrust.NewOpID()}
	ctx.Data["Title"] = ctx.Tr("asc_shell.ia_title")
	ctx.Data["PageIsAdminAscAgents"] = true
	ctx.Data["AscInstanceAgents"] = p
	status := http.StatusOK
	defer func() { ctx.HTML(status, tplAscAgents) }()

	if !asctrust.HasFeature(ctx, asctrust.FeatureOrgSettings) {
		p.Unsupported = true
		return
	}
	conn, err := asctrust.Conn()
	if err != nil {
		log.Error("asc agents: no connection to asc serve: %v", err)
		p.Problem, status = ctx.Locale.TrString("asc_trust.err.unavailable"), http.StatusBadGateway
		return
	}
	callCtx, release, err := asctrust.UserCall(ctx, ctx.Doer, asctrust.CallInstance, nil)
	if err != nil {
		f := trustweb.FailureOf("agents credential", err)
		p.Problem, status = f.Localized(ctx), f.Status
		return
	}
	defer release()
	s, err := asctrust.LoadInstanceAgents(callCtx, conn)
	if err != nil {
		f := trustweb.FailureOf("agents load", err)
		p.Problem, status = f.Localized(ctx), f.Status
		return
	}
	p.Settings = s
}

// AscAgentsSave serves POST /admin/asc/agents with the fields allow_agents ("1" is on), budget_usd and op.
func AscAgentsSave(ctx *context.Context) {
	if !ascAgentsGuard(ctx) || !trustweb.ParseForm(ctx, 8<<10) {
		return
	}
	to := setting.AppSubURL + "/admin/asc/agents"
	fail := func(msg string) {
		ctx.Flash.Error(msg)
		ctx.Redirect(to)
	}
	s, err := asctrust.InstanceAgentsFromForm(ctx.Req.PostFormValue)
	if err != nil {
		fail(trustweb.FailureOf("agents form", err).Localized(ctx))
		return
	}
	conn, err := asctrust.Conn()
	if err != nil {
		fail(ctx.Locale.TrString("asc_trust.err.unavailable"))
		return
	}
	callCtx, release, err := asctrust.UserCall(ctx, ctx.Doer, asctrust.CallInstance, nil)
	if err != nil {
		fail(trustweb.FailureOf("agents credential", err).Localized(ctx))
		return
	}
	defer release()
	if _, err := asctrust.SetInstanceAgents(callCtx, conn, s, ctx.Req.PostFormValue("op")); err != nil {
		fail(trustweb.FailureOf("agents save", err).Localized(ctx))
		return
	}
	ctx.Flash.Success(ctx.Locale.TrString("asc_shell.ia_saved"))
	ctx.Redirect(to)
}
