// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package setting

import (
	gocontext "context"
	"net/http"

	"forgejo.org/modules/base"
	"forgejo.org/modules/log"
	"forgejo.org/modules/setting"
	"forgejo.org/routers/web/shared/trustweb"
	"forgejo.org/services/asctrust"
	"forgejo.org/services/context"

	"google.golang.org/grpc"
)

// The agents and merge defaults of an organisation (docs/BUILDOUT.md section 28), served by asc serve's OrgSettings:
//
//	GET  /org/{org}/settings/asc/agents                    the agents card, the agents, the form for a new one
//	POST /org/{org}/settings/asc/agents/policy             allow agents, need a person, monthly ceiling
//	POST /org/{org}/settings/asc/agents/new                create an agent
//	POST /org/{org}/settings/asc/agents/{name}/update      change an agent
//	POST /org/{org}/settings/asc/agents/{name}/state       pause or resume (field state)
//	POST /org/{org}/settings/asc/agents/{name}/delete      delete
//	GET  /org/{org}/settings/asc/merge                     the merge requirements every repository inherits
//	POST /org/{org}/settings/asc/merge                     replace them
//
// Only an owner of the organisation, or a site administrator, may use them, and the organisation is the one of the
// route. The calls are made as the person: the hub decides first (asctrust.UserCallOrg) and signs a claim with the role
// of an owner, and asc decides again.
const (
	tplSettingsAscAgents base.TplName = "org/settings/asc_agents"
	tplSettingsAscMerge  base.TplName = "org/settings/asc_merge"

	maxAscOrgForm = 64 << 10
)

// ascOrgGuard fails closed and returns the name of the organisation.
func ascOrgGuard(ctx *context.Context) (string, bool) {
	if !setting.Asc.Enabled || ctx.Org == nil || ctx.Org.Organization == nil {
		ctx.NotFound("asc org settings", nil)
		return "", false
	}
	if !ctx.IsSigned || ctx.Doer == nil || !(ctx.Org.IsOwner || ctx.Doer.IsAdmin) {
		ctx.Error(http.StatusForbidden)
		return "", false
	}
	return ctx.Org.Organization.Name, true
}

// ascOrgCall returns the connection and the context of a call made as the person for the organisation. A failure is a
// sentence and a status.
func ascOrgCall(ctx *context.Context, op string) (conn grpc.ClientConnInterface, callCtx gocontext.Context, release func(), problem string, status int) {
	c, err := asctrust.Conn()
	if err != nil {
		log.Error("asc %s: no connection to asc serve: %v", op, err)
		return nil, nil, nil, ctx.Locale.TrString("asc_trust.err.unavailable"), http.StatusBadGateway
	}
	cc, rel, err := asctrust.UserCallOrg(ctx, ctx.Doer, ctx.Org.Organization.AsUser())
	if err != nil {
		f := trustweb.FailureOf(op+" credential", err)
		return nil, nil, nil, f.Localized(ctx), f.Status
	}
	return c, cc, rel, "", 0
}

func ascOrgBack(ctx *context.Context, to, success, problem string) {
	if problem != "" {
		ctx.Flash.Error(problem)
	} else if success != "" {
		ctx.Flash.Success(success)
	}
	ctx.Redirect(to)
}

// ascAgentOps are the operation ids of the forms of one agent.
type ascAgentOps struct{ Update, State, Delete string }

type ascAgentsPage struct {
	Base        string
	Settings    *asctrust.OrgSettings
	Agents      []asctrust.OrgAgent
	Problem     string
	Unsupported bool
	PolicyOp    string
	NewOp       string
	Ops         map[string]ascAgentOps
	MaxAgents   int
}

// AscAgents serves GET /org/{org}/settings/asc/agents.
func AscAgents(ctx *context.Context) {
	org, ok := ascOrgGuard(ctx)
	if !ok {
		return
	}
	trustweb.NoStore(ctx)
	p := &ascAgentsPage{Base: setting.AppSubURL + "/org/" + org + "/settings/asc/agents", PolicyOp: asctrust.NewOpID(), NewOp: asctrust.NewOpID(), Ops: map[string]ascAgentOps{}, MaxAgents: asctrust.MaxOrgAgents}
	ctx.Data["Title"] = ctx.Tr("asc_shell.oa_title")
	ctx.Data["PageIsSettingsAscAgents"] = true
	ctx.Data["AscOrgAgents"] = p
	status := http.StatusOK
	defer func() { ctx.HTML(status, tplSettingsAscAgents) }()

	if !asctrust.HasFeature(ctx, asctrust.FeatureOrgSettings) {
		p.Unsupported = true
		return
	}
	conn, callCtx, release, problem, st := ascOrgCall(ctx, "org agents")
	if problem != "" {
		p.Problem, status = problem, st
		return
	}
	defer release()
	s, err := asctrust.LoadOrgSettings(callCtx, conn, org)
	if err != nil {
		f := trustweb.FailureOf("org settings", err)
		p.Problem, status = f.Localized(ctx), f.Status
		return
	}
	agents, err := asctrust.ListOrgAgents(callCtx, conn, org)
	if err != nil {
		f := trustweb.FailureOf("org agents", err)
		p.Problem, status = f.Localized(ctx), f.Status
		return
	}
	p.Settings, p.Agents = s, agents
	for _, a := range agents {
		p.Ops[a.Name] = ascAgentOps{Update: asctrust.NewOpID(), State: asctrust.NewOpID(), Delete: asctrust.NewOpID()}
	}
}

// ascOrgPost runs the common start of a POST of the agents and merge forms: guard, form, call. It returns false when
// the answer is written.
func ascOrgPost(ctx *context.Context, back string, run func(callCtx gocontext.Context, conn grpc.ClientConnInterface, org string) (string, error)) {
	org, ok := ascOrgGuard(ctx)
	if !ok || !trustweb.ParseForm(ctx, maxAscOrgForm) {
		return
	}
	to := setting.AppSubURL + "/org/" + org + "/settings/asc/" + back
	conn, callCtx, release, problem, _ := ascOrgCall(ctx, "org settings")
	if problem != "" {
		ascOrgBack(ctx, to, "", problem)
		return
	}
	defer release()
	done, err := run(callCtx, conn, org)
	if err != nil {
		ascOrgBack(ctx, to, "", trustweb.FailureOf("org settings change", err).Localized(ctx))
		return
	}
	ascOrgBack(ctx, to, ctx.Locale.TrString(done), "")
}

// AscAgentsPolicy serves POST /org/{org}/settings/asc/agents/policy.
func AscAgentsPolicy(ctx *context.Context) {
	ascOrgPost(ctx, "agents", func(callCtx gocontext.Context, conn grpc.ClientConnInterface, org string) (string, error) {
		p, err := asctrust.AgentPolicyFromForm(ctx.Req.PostFormValue)
		if err != nil {
			return "", err
		}
		_, err = asctrust.SetAgentPolicy(callCtx, conn, org, p, ctx.Req.PostFormValue("op"))
		return "asc_shell.oa_policy_saved", err
	})
}

// AscAgentsNew serves POST /org/{org}/settings/asc/agents/new.
func AscAgentsNew(ctx *context.Context) {
	ascOrgPost(ctx, "agents", func(callCtx gocontext.Context, conn grpc.ClientConnInterface, org string) (string, error) {
		a, err := asctrust.NewAgentFromForm(ctx.Req.PostFormValue)
		if err != nil {
			return "", err
		}
		_, err = asctrust.CreateOrgAgent(callCtx, conn, org, a, ctx.Req.PostFormValue("op"))
		return "asc_shell.oa_created", err
	})
}

// AscAgentsUpdate serves POST /org/{org}/settings/asc/agents/{name}/update.
func AscAgentsUpdate(ctx *context.Context) {
	ascOrgPost(ctx, "agents", func(callCtx gocontext.Context, conn grpc.ClientConnInterface, org string) (string, error) {
		_, err := asctrust.UpdateOrgAgent(callCtx, conn, org, ctx.Params(":name"), ctx.Req.PostFormValue, "", ctx.Req.PostFormValue("op"))
		return "asc_shell.oa_updated", err
	})
}

// AscAgentsState serves POST /org/{org}/settings/asc/agents/{name}/state with the field state ("active" or "paused").
func AscAgentsState(ctx *context.Context) {
	ascOrgPost(ctx, "agents", func(callCtx gocontext.Context, conn grpc.ClientConnInterface, org string) (string, error) {
		state := ctx.Req.PostFormValue("state")
		if state != "active" && state != "paused" {
			return "", asctrust.ErrBadInput
		}
		_, err := asctrust.UpdateOrgAgent(callCtx, conn, org, ctx.Params(":name"), nil, state, ctx.Req.PostFormValue("op"))
		if state == "paused" {
			return "asc_shell.oa_paused", err
		}
		return "asc_shell.oa_resumed", err
	})
}

// AscAgentsDelete serves POST /org/{org}/settings/asc/agents/{name}/delete.
func AscAgentsDelete(ctx *context.Context) {
	ascOrgPost(ctx, "agents", func(callCtx gocontext.Context, conn grpc.ClientConnInterface, org string) (string, error) {
		return "asc_shell.oa_deleted", asctrust.DeleteOrgAgent(callCtx, conn, org, ctx.Params(":name"), ctx.Req.PostFormValue("op"))
	})
}

type ascMergePage struct {
	Base        string
	Settings    *asctrust.OrgSettings
	Problem     string
	Unsupported bool
	Op          string
	Strategies  []string
}

// AscMerge serves GET /org/{org}/settings/asc/merge.
func AscMerge(ctx *context.Context) {
	org, ok := ascOrgGuard(ctx)
	if !ok {
		return
	}
	trustweb.NoStore(ctx)
	p := &ascMergePage{Base: setting.AppSubURL + "/org/" + org + "/settings/asc/merge", Op: asctrust.NewOpID(), Strategies: asctrust.OrgMergeStrategies}
	ctx.Data["Title"] = ctx.Tr("asc_shell.om_title")
	ctx.Data["PageIsSettingsAscMerge"] = true
	ctx.Data["AscOrgMerge"] = p
	status := http.StatusOK
	defer func() { ctx.HTML(status, tplSettingsAscMerge) }()

	if !asctrust.HasFeature(ctx, asctrust.FeatureOrgSettings) {
		p.Unsupported = true
		return
	}
	conn, callCtx, release, problem, st := ascOrgCall(ctx, "org merge")
	if problem != "" {
		p.Problem, status = problem, st
		return
	}
	defer release()
	s, err := asctrust.LoadOrgSettings(callCtx, conn, org)
	if err != nil {
		f := trustweb.FailureOf("org settings", err)
		p.Problem, status = f.Localized(ctx), f.Status
		return
	}
	p.Settings = s
}

// AscMergeSave serves POST /org/{org}/settings/asc/merge.
func AscMergeSave(ctx *context.Context) {
	ascOrgPost(ctx, "merge", func(callCtx gocontext.Context, conn grpc.ClientConnInterface, org string) (string, error) {
		m, err := asctrust.MergeDefaultsFromForm(ctx.Req.PostFormValue)
		if err != nil {
			return "", err
		}
		_, err = asctrust.SetMergeDefaults(callCtx, conn, org, m, ctx.Req.PostFormValue("op"))
		return "asc_shell.om_saved", err
	})
}
