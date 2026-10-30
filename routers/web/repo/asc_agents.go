// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	gocontext "context"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	git_model "forgejo.org/models/git"
	"forgejo.org/models/unit"
	"forgejo.org/modules/ascclient"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/base"
	"forgejo.org/modules/log"
	"forgejo.org/services/ascagentui"
	"forgejo.org/services/context"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Agent runs: start one from a task, watch it, steer it, stop it, and review what it produced.
//
//	GET  /{owner}/{repo}/asc/agents                      the runs (filters: state, mine, agent; paged by asc's token)
//	GET  /{owner}/{repo}/asc/agents/new                  the task form: prompt, agent, branch, AI key and model, budget
//	POST /{owner}/{repo}/asc/agents/new                  start the run
//	GET  /{owner}/{repo}/asc/agents/runs/{id}            the run: state, live activity, budget, access, result stack
//	GET  /{owner}/{repo}/asc/agents/runs/{id}/events     the poll of the live page (JSON; see asc_agents_feed.go)
//	POST /{owner}/{repo}/asc/agents/runs/{id}/cancel     stop the run
//	POST /{owner}/{repo}/asc/agents/runs/{id}/input      answer a question of the agent, or steer it
//
// All of it needs the person's agent features switch (ascagentui.FeaturesEnabled); see asc_agents_common.go for
// the trust rules. What the form sends is checked here (ascagentui has the checks); what asc enforces on top is
// the repository policy, the token scope and the budgets, never the agent. An agent never pushes trunk: its output
// is a stack of changes, which this page links to the normal review page.

const (
	tplAscAgents          base.TplName = "repo/asc_agents"
	tplAscAgentsNew       base.TplName = "repo/asc_agents_new"
	tplAscAgentsRun       base.TplName = "repo/asc_agents_run"
	tplAscAgentsTokens    base.TplName = "repo/asc_agents_tokens"
	ascAgentsPageSize                  = 30
	ascAgentsMaxKeys                   = 8
	ascAgentsMaxModels                 = 200
	ascAgentsDefaultAgent              = "coder"
)

// ascAgentRunRow is one run as drawn in a list.
type ascAgentRunRow struct {
	ID        uint64
	Title     string
	Agent     string
	Model     string
	CreatedBy string
	State     ascagentui.State
	Created   time.Time
	Finished  time.Time
	Changes   int
	Tokens    string
	Cost      string
	Failure   string
	Link      string
	// Initials is the mark of the agent avatar: its first letter and its trailing number ("c2" for coder-2).
	Initials string
	// Progress is "3 of 4 steps"; TokensPct the share of the token limit used (0 without a limit).
	Progress    ascagentui.Progress
	TokensLimit string
	TokensPct   int
	TokensWarn  bool
	Duration    string
	Paused      bool
	Pending     int
	Task        string
	TaskHidden  bool
	ByInitials  string
	Workflow    uint64
	Active      bool
}

// ascAgentInitials draws the avatar mark of an agent name: the first letter and a trailing number, else the first two letters.
func ascAgentInitials(name string) string {
	r := []rune(strings.ToLower(name))
	if len(r) == 0 {
		return "?"
	}
	if last := r[len(r)-1]; len(r) > 1 && last >= '0' && last <= '9' {
		return string([]rune{r[0], last})
	}
	if len(r) > 1 {
		return string(r[:2])
	}
	return string(r)
}

// ascAgentRunOf checks a run that asc sent for repository `name` and turns it into a row. A run of another
// repository, without an id, or with an id other than the one asked for (when wantID is not 0) is refused.
func ascAgentRunOf(run *ascv1.AgentRun, name, runsLink string, wantID uint64) (ascAgentRunRow, bool) {
	if run == nil || run.GetId() == 0 || run.GetRepo() != name || (wantID != 0 && run.GetId() != wantID) {
		return ascAgentRunRow{}, false
	}
	row := ascAgentRunRow{
		ID:        run.GetId(),
		Title:     ascagentui.CleanLine(run.GetTitle(), ascagentui.MaxTitleBytes),
		Agent:     ascagentui.SafeName(run.GetAgent()),
		Model:     ascagentui.CleanLine(run.GetModel(), ascagentui.MaxModelBytes),
		CreatedBy: ascagentui.SafeName(run.GetCreatedBy()),
		State:     ascagentui.RunState(run.GetState()),
		Created:   ascagentui.Time(run.GetCreated()),
		Finished:  ascagentui.Time(run.GetFinished()),
		Changes:   len(run.GetChanges()),
		Tokens:    ascagentui.FormatCount(run.GetUsage().GetTokens()),
		Cost:      ascagentui.FormatUSD(run.GetUsage().GetCostMicroUsd()),
		Link:      runsLink + "/runs/" + strconv.FormatUint(run.GetId(), 10),
	}
	row.Initials = ascAgentInitials(row.Agent)
	row.Progress = ascagentui.ProgressOf(run.GetProgress())
	if lim := run.GetLimits().GetMaxTokens(); lim > 0 {
		row.TokensLimit = ascagentui.FormatCount(lim)
		row.TokensPct = ascagentui.Percent(run.GetUsage().GetTokens(), lim)
		row.TokensWarn = row.TokensPct >= 80
	}
	if sec := run.GetUsage().GetRuntimeSeconds(); sec > 0 {
		row.Duration = ascagentui.FormatDuration(uint64(sec))
	}
	row.Paused = run.GetPaused()
	row.Pending = int(min(run.GetPendingRequests(), 1000))
	// The task text and the failure message are private to the creator and the administrators (ascAgentsReveal).
	row.TaskHidden = run.GetTask() != ""
	row.ByInitials = ascAgentInitials(row.CreatedBy)
	row.Workflow = run.GetWorkflowRunId()
	row.Active = row.State.Active
	if row.Title == "" {
		row.Title = "Run " + strconv.FormatUint(row.ID, 10)
	}
	if run.GetFailureMessage() != "" || run.GetFailureCode() != "" {
		row.Failure = ascagentui.SafeCode(run.GetFailureCode())
	}
	return row, true
}

// ascAgentsReveal puts the private text of a run into its row when the viewer may see it (ascAgentsMaySeeRun).
func ascAgentsReveal(ctx *context.Context, row *ascAgentRunRow, run *ascv1.AgentRun) {
	if !ascAgentsMaySeeRun(ctx, run.GetCreatedById()) {
		return
	}
	row.Task = ascagentui.CleanLine(run.GetTask(), 300)
	row.TaskHidden = false
	if m := ascagentui.CleanLine(run.GetFailureMessage(), 300); m != "" {
		row.Failure = m
	}
}

// ascAgentWaitingRows are the runs on the page that wait for a person: the "Needs you" panel.
func ascAgentWaitingRows(rows []ascAgentRunRow) []ascAgentRunRow {
	var out []ascAgentRunRow
	for _, r := range rows {
		if r.State.Slug == "waiting" {
			out = append(out, r)
		}
	}
	return out
}

func (e *ascAgentsEnv) runsLink() string { return e.link + "/asc/agents" }

func ascAgentsNav(ctx *context.Context, tab string) {
	ctx.Data["PageIsAscAgentArea"] = true
	ctx.Data["AscAgentsTab"] = tab
}

// AscAgents serves `GET /{owner}/{repo}/asc/agents`.
func AscAgents(ctx *context.Context) {
	env, ok := ascAgentsGate(ctx, ascAgentsOpts{level: ascAgentsRead})
	if !ok {
		return
	}
	ctx.Data["Title"] = "Agents"
	ascAgentsNav(ctx, "runs")

	stateSlug := ctx.FormString("state")
	states, valid := ascagentui.RunStateFilter(stateSlug)
	mine := ctx.FormBool("mine")
	agent := ctx.FormTrim("agent")
	token, tokenOK := ascAgentsPageToken(ctx.FormString("page"))
	if !valid || !tokenOK || (agent != "" && !ascagentui.ValidAgentName(agent)) {
		ctx.Data["AscAgentsFailure"] = ascagentui.Classify(ascInvalidArgument())
		ctx.HTML(http.StatusBadRequest, tplAscAgents)
		return
	}
	ctx.Data["AscAgentsFilterState"] = stateSlug
	ctx.Data["AscAgentsFilterMine"] = mine
	ctx.Data["AscAgentsFilterAgent"] = agent
	ctx.Data["AscAgentsStateFilters"] = ascagentui.RunStateSlugs
	q := ctx.FormTrim("q")
	if len(q) > 80 || strings.ContainsAny(q, "\x00\r\n") {
		q = ""
	}
	ctx.Data["AscAgentsFilterQ"] = q

	req := &ascv1.ListAgentRunsRequest{Repo: env.ref(), States: states, Agent: agent, Limit: ascAgentsPageSize, PageToken: token}
	if mine {
		req.CreatedBy = ctx.Doer.Name // always the signed-in person, never a value of the request
	}
	conn, err := ascRPCConn()
	if err != nil {
		log.Error("asc agents: no connection to asc serve: %v", err)
		ascAgentsFail(ctx, tplAscAgents, "connection", err)
		return
	}
	callCtx, cancel := ascAgentsReadCtx(ctx)
	defer cancel()
	resp, err := ascv1.NewAgentsClient(conn).ListRuns(callCtx, req)
	if err != nil {
		ascAgentsFail(ctx, tplAscAgents, "ListRuns", err)
		return
	}
	rows := make([]ascAgentRunRow, 0, len(resp.GetRuns()))
	creators := map[uint64]uint64{}
	active, waiting, done := 0, 0, 0
	for _, run := range resp.GetRuns() {
		row, ok := ascAgentRunOf(run, env.name, env.runsLink(), 0)
		if !ok {
			log.Error("asc agents: ListRuns answered a run that is not of this repository")
			ascAgentsFail(ctx, tplAscAgents, "ListRuns", errAscAgentsBadAnswer)
			return
		}
		ascAgentsReveal(ctx, &row, run)
		creators[row.ID] = run.GetCreatedById()
		if row.State.Active {
			active++
		}
		switch row.State.Slug {
		case "waiting":
			waiting++
		case "succeeded":
			done++
		}
		rows = append(rows, row)
	}
	ascAgentsLoadBoard(ctx, env, callCtx, conn, creators)
	shown := ascAgentsFilterRows(rows, q)
	ctx.Data["AscAgentsRuns"] = shown
	ctx.Data["AscAgentsLoaded"] = len(rows)
	ctx.Data["AscAgentsActive"] = active
	ctx.Data["AscAgentsWaiting"] = waiting
	ctx.Data["AscAgentsDone"] = done
	ctx.Data["AscAgentsWaitingRuns"] = ascAgentWaitingRows(rows)
	if next, ok := ascAgentsPageToken(resp.GetNextPageToken()); ok && next != "" {
		q := url.Values{}
		if stateSlug != "" {
			q.Set("state", stateSlug)
		}
		if mine {
			q.Set("mine", "1")
		}
		if agent != "" {
			q.Set("agent", agent)
		}
		if search := ctx.FormTrim("q"); search != "" && len(search) <= 80 {
			q.Set("q", search)
		}
		q.Set("page", next)
		ctx.Data["AscAgentsNext"] = env.runsLink() + "?" + q.Encode()
	}
	ctx.HTML(http.StatusOK, tplAscAgents)
}

// ---------------------------------------------------------------------------------------------- the task form

// ascAgentForm is the task form as typed, shown again on an error.
type ascAgentForm struct {
	Title     string
	Task      string
	Agent     string
	Target    string
	Key       string
	Model     string
	Paths     string
	PlanFirst bool
	Budget    ascagentui.BudgetInput
	OpID      string
	Errors    map[string]string
	Summary   string
}

// ascAgentKeyOption is one AI key in the picker. The key itself never reaches the hub: asc answers with a label,
// the provider, the default model and a state.
type ascAgentKeyOption struct {
	ID       string
	Label    string
	Provider string
	Model    string
	Owner    string
	Usable   bool
	Note     string
	Models   []string
}

func ascAgentProviderName(p ascv1.AiProvider) string {
	switch p {
	case ascv1.AiProvider_AI_PROVIDER_ANTHROPIC:
		return "Anthropic"
	case ascv1.AiProvider_AI_PROVIDER_OPENAI:
		return "OpenAI"
	case ascv1.AiProvider_AI_PROVIDER_GOOGLE:
		return "Google"
	case ascv1.AiProvider_AI_PROVIDER_OPENAI_COMPATIBLE:
		return "OpenAI-compatible"
	}
	return "Unknown provider"
}

// ascAgentKeyOf checks one key that asc listed. The picker shows only what the owner may see: label, provider,
// default model and state. A key whose id is malformed is dropped.
func ascAgentKeyOf(k *ascv1.AiKey, owner string) (ascAgentKeyOption, bool) {
	if k == nil || !ascagentui.ValidKeyID(k.GetId()) {
		return ascAgentKeyOption{}, false
	}
	opt := ascAgentKeyOption{
		ID:       k.GetId(),
		Label:    ascagentui.CleanLine(k.GetLabel(), 64),
		Provider: ascAgentProviderName(k.GetProvider()),
		Owner:    owner,
	}
	if m := k.GetDefaultModel(); ascagentui.ValidModel(m) {
		opt.Model = m
	}
	if opt.Label == "" {
		opt.Label = opt.ID
	}
	switch k.GetState() {
	case ascv1.AiKeyState_AI_KEY_STATE_ACTIVE:
		opt.Usable = true
	case ascv1.AiKeyState_AI_KEY_STATE_UNTESTED:
		opt.Note = "not tested yet"
	case ascv1.AiKeyState_AI_KEY_STATE_FAILED:
		opt.Note = "the provider refused it"
	case ascv1.AiKeyState_AI_KEY_STATE_CAPPED:
		opt.Note = "monthly cap reached"
	case ascv1.AiKeyState_AI_KEY_STATE_DISABLED:
		opt.Note = "disabled"
	default:
		opt.Note = "unknown state"
	}
	return opt, true
}

// ascAgentKeyOptions lists the keys the person can pick: those of the repository's owner and their own. It uses a
// one-use token of the person, because asc shows keys to the admins of their owner only; a key the person may not
// list is simply absent, and the form then offers "the repository default". It never fails the page.
func ascAgentKeyOptions(ctx *context.Context, callCtx gocontext.Context, api ascv1.AiProvidersClient) []ascAgentKeyOption {
	repo := ctx.Repo.Repository
	if err := repo.LoadOwner(ctx); err != nil {
		return nil
	}
	type owner struct {
		ref   *ascv1.OwnerRef
		label string
	}
	var owners []owner
	kind := ascv1.OwnerKind_OWNER_KIND_USER
	if repo.Owner.IsOrganization() {
		kind = ascv1.OwnerKind_OWNER_KIND_ORG
	}
	owners = append(owners, owner{&ascv1.OwnerRef{Kind: kind, Name: repo.Owner.Name}, repo.Owner.Name})
	if repo.OwnerID != ctx.Doer.ID {
		owners = append(owners, owner{&ascv1.OwnerRef{Kind: ascv1.OwnerKind_OWNER_KIND_USER, Name: ctx.Doer.Name}, ctx.Doer.Name})
	}
	var out []ascAgentKeyOption
	seen := map[string]bool{}
	for _, o := range owners {
		resp, err := api.ListKeys(callCtx, &ascv1.ListAiKeysRequest{Owner: o.ref})
		if err != nil {
			continue
		}
		for _, k := range resp.GetKeys() {
			opt, ok := ascAgentKeyOf(k, o.label)
			if !ok || seen[opt.ID] || len(out) >= ascAgentsMaxKeys {
				continue
			}
			seen[opt.ID] = true
			if opt.Usable {
				if mr, err := api.ListModels(callCtx, &ascv1.ListAiModelsRequest{Id: opt.ID}); err == nil {
					for _, m := range mr.GetModels() {
						if ascagentui.ValidModel(m.GetId()) && len(opt.Models) < ascAgentsMaxModels {
							opt.Models = append(opt.Models, m.GetId())
						}
					}
				}
			}
			out = append(out, opt)
		}
	}
	return out
}

// ascAgentPolicy is the part of the repository policy that tells what an agent may do.
type ascAgentPolicy struct {
	Known      bool
	Scope      []string
	Denied     []string
	MaxChanges uint32
	MaxPaths   uint32
}

func ascAgentPolicyOf(callCtx gocontext.Context, ref *ascv1.RepoRef) ascAgentPolicy {
	conn, err := ascRPCConn()
	if err != nil {
		return ascAgentPolicy{}
	}
	resp, err := ascv1.NewPolicyClient(conn).GetSource(callCtx, &ascv1.GetPolicySourceRequest{Repo: ref})
	if err != nil || !resp.GetPresent() || resp.GetParsed().GetError() != "" {
		return ascAgentPolicy{}
	}
	a := resp.GetParsed().GetAgents()
	p := ascAgentPolicy{Known: true, MaxChanges: a.GetMaxChangesPerRun(), MaxPaths: a.GetMaxPathsPerRun()}
	for _, g := range a.GetDefaultScope() {
		if c := ascagentui.CleanLine(g, ascagentui.MaxGlobBytes); c != "" && len(p.Scope) < 32 {
			p.Scope = append(p.Scope, c)
		}
	}
	for _, g := range a.GetDeniedPaths() {
		if c := ascagentui.CleanLine(g, ascagentui.MaxGlobBytes); c != "" && len(p.Denied) < 32 {
			p.Denied = append(p.Denied, c)
		}
	}
	return p
}

// ascInvalidArgument is the error of a query string the hub refuses before calling asc.
func ascInvalidArgument() error { return status.Error(codes.InvalidArgument, "bad query") }

// AscAgentsNew serves `GET /{owner}/{repo}/asc/agents/new`.
func AscAgentsNew(ctx *context.Context) {
	env, ok := ascAgentsGate(ctx, ascAgentsOpts{level: ascAgentsWrite, notArchived: true})
	if !ok {
		return
	}
	form := ascAgentForm{Agent: ascAgentsDefaultAgent, Target: ctx.Repo.Repository.DefaultBranch, OpID: ascAgentsNewOpID()}
	ascAgentsPrefill(ctx, &form)
	ascAgentsRenderForm(ctx, env, &form, http.StatusOK)
}

// ascAgentsPrefill fills the task form from the address, so a link can start the form for a failing check: title, task,
// agent and target. Nothing is trusted: the same checks run when the form is sent, and a value that does not fit is left
// out here.
func ascAgentsPrefill(ctx *context.Context, form *ascAgentForm) {
	if t, ok := ascagentui.CheckFreeText(ctx.FormString("task"), ascagentui.MaxTaskBytes); ok {
		form.Task = t
	}
	if t, ok := ascagentui.CheckLine(ctx.FormString("title"), ascagentui.MaxTitleBytes, true); ok {
		form.Title = t
	}
	if a := ctx.FormTrim("agent"); a != "" && ascagentui.ValidAgentName(a) {
		form.Agent = a
	}
	if b := ctx.FormTrim("target"); b != "" {
		if br, err := git_model.GetBranch(ctx, ctx.Repo.Repository.ID, b); err == nil && !br.IsDeleted {
			form.Target = b
		}
	}
}

// ascAgentsRenderForm draws the task form with the picker and the policy panel.
func ascAgentsRenderForm(ctx *context.Context, env *ascAgentsEnv, form *ascAgentForm, status int) {
	ctx.Data["Title"] = "Ask an agent"
	ascAgentsNav(ctx, "new")
	ctx.Data["AscAgentForm"] = form
	ctx.Data["AscBudgetFields"] = ascagentui.BudgetFields
	ctx.Data["AscAgentsMaxTask"] = ascagentui.MaxTaskBytes

	conn, err := ascRPCConn()
	if err != nil {
		log.Error("asc agents: no connection to asc serve: %v", err)
		ctx.Data["AscAgentsFailure"] = ascagentui.Classify(err)
		ctx.HTML(http.StatusBadGateway, tplAscAgentsNew)
		return
	}
	readCtx, cancelRead := ascAgentsReadCtx(ctx)
	defer cancelRead()
	ctx.Data["AscAgentPolicy"] = ascAgentPolicyOf(readCtx, env.ref())

	if uctx, done, ok := ascAgentsWriteCtx(ctx, ""); ok {
		defer done()
		keys := ascAgentKeyOptions(ctx, uctx, ascv1.NewAiProvidersClient(conn))
		ctx.Data["AscAgentKeys"] = keys
		ascAgentsLoadNewExtras(ctx, env, readCtx, uctx, form)
		models := map[string][]string{}
		for _, k := range keys {
			if len(k.Models) > 0 {
				models[k.ID] = k.Models
			}
		}
		if len(models) > 0 {
			if b, err := json.Marshal(models); err == nil {
				ctx.Data["AscAgentModelsJSON"] = string(b)
			}
		}
	} else {
		return // ascAgentsWriteCtx answered
	}
	ctx.HTML(status, tplAscAgentsNew)
}

// ascAgentsLimitBody bounds the form body a handler reads into memory.
func ascAgentsLimitBody(ctx *context.Context) {
	if ctx.Req.Body != nil && ctx.Req.Body != http.NoBody {
		ctx.Req.Body = http.MaxBytesReader(ctx.Resp, ctx.Req.Body, ascAgentsMaxFormBytes)
	}
}

// AscAgentsCreate serves `POST /{owner}/{repo}/asc/agents/new`.
func AscAgentsCreate(ctx *context.Context) {
	env, ok := ascAgentsGate(ctx, ascAgentsOpts{level: ascAgentsWrite, notArchived: true})
	if !ok {
		return
	}
	ascAgentsLimitBody(ctx)
	form := ascAgentForm{
		Title:     ctx.FormString("title"),
		Task:      ctx.FormString("task"),
		Agent:     ctx.FormTrim("agent"),
		Target:    ctx.FormTrim("target"),
		Key:       ctx.FormTrim("key"),
		Model:     ctx.FormTrim("model"),
		Paths:     ctx.FormString("paths"),
		PlanFirst: ctx.FormBool("plan_first"),
		OpID:      ctx.FormTrim("op"),
		Budget: ascagentui.BudgetInput{
			Changes: ctx.FormTrim(ascagentui.BudgetFields.Changes),
			Tokens:  ctx.FormTrim(ascagentui.BudgetFields.Tokens),
			CostUSD: ctx.FormTrim(ascagentui.BudgetFields.CostUSD),
			Paths:   ctx.FormTrim(ascagentui.BudgetFields.Paths),
			Minutes: ctx.FormTrim(ascagentui.BudgetFields.Minutes),
		},
		Errors: map[string]string{},
	}
	if form.OpID == "" {
		form.OpID = ascAgentsNewOpID()
	}
	if form.Agent == "" {
		form.Agent = ascAgentsDefaultAgent
	}
	if form.Target == "" {
		form.Target = ctx.Repo.Repository.DefaultBranch
	}

	req := &ascv1.CreateAgentRunRequest{Repo: env.ref(), Agent: form.Agent, PlanFirst: form.PlanFirst}
	if t, ok := ascagentui.CheckFreeText(form.Task, ascagentui.MaxTaskBytes); ok {
		req.Task = t
	} else {
		form.Errors["task"] = "Describe the task in 1 to 65,536 bytes of text."
	}
	if t, ok := ascagentui.CheckLine(form.Title, ascagentui.MaxTitleBytes, true); ok {
		req.Title = t
	} else {
		form.Errors["title"] = "The title is at most 200 characters on one line."
	}
	if !ascagentui.ValidAgentName(form.Agent) {
		form.Errors["agent"] = "An agent name is 1 to 40 characters: a to z, 0 to 9 and hyphens."
	}
	if b, err := git_model.GetBranch(ctx, ctx.Repo.Repository.ID, form.Target); err != nil || b.IsDeleted {
		form.Errors["target"] = "Choose a branch of this repository."
	} else {
		req.Target = form.Target
	}
	if form.Key != "" {
		if ascagentui.ValidKeyID(form.Key) {
			req.AiKeyId = form.Key
		} else {
			form.Errors["key"] = "Choose a key from the list."
		}
	}
	if form.Model != "" {
		if ascagentui.ValidModel(form.Model) {
			req.Model = form.Model
		} else {
			form.Errors["model"] = "That is not a model name."
		}
	}
	if globs, ok := ascagentui.ParseGlobs(form.Paths); ok {
		req.Paths = globs
	} else {
		form.Errors["paths"] = "Give up to 16 relative path patterns, one per line, with no \"..\"."
	}
	if limits, err := ascagentui.ParseBudget(form.Budget); err == nil {
		req.Limits = limits
	} else if be, ok := err.(*ascagentui.BudgetError); ok {
		form.Errors[be.Field] = "Enter a whole number, or leave it empty for the policy default."
		if be.Field == ascagentui.BudgetFields.CostUSD {
			form.Errors[be.Field] = "Enter dollars such as 2.50, or leave it empty for the policy default."
		}
	}
	if len(form.Errors) > 0 {
		form.Summary = "Some fields need another look."
		ascAgentsRenderForm(ctx, env, &form, http.StatusBadRequest)
		return
	}

	conn, err := ascRPCConn()
	if err != nil {
		log.Error("asc agents: no connection to asc serve: %v", err)
		form.Summary = ascagentui.Classify(err).Message
		ascAgentsRenderForm(ctx, env, &form, http.StatusBadGateway)
		return
	}
	callCtx, done, ok := ascAgentsWriteCtx(ctx, form.OpID)
	if !ok {
		return
	}
	resp, err := ascv1.NewAgentsClient(conn).CreateRun(callCtx, req)
	done()
	if err != nil {
		f := ascagentui.Classify(err)
		if f == nil || ctx.Req.Context().Err() != nil {
			return
		}
		if f.Unavailable {
			log.Error("asc agents: CreateRun failed: %s", ascagentuiLogLine(f))
		}
		log.Warn("asc agents: CreateRun refused: %v", err)
		form.Summary = f.Message
		for _, d := range f.Details {
			form.Summary += " " + d + "."
		}
		status := f.HTTP
		if f.Disabled {
			ctx.Data["AscAgentsDisabled"] = true
		}
		ascAgentsRenderForm(ctx, env, &form, status)
		return
	}
	row, ok := ascAgentRunOf(resp.GetRun(), env.name, env.runsLink(), 0)
	if !ok {
		log.Error("asc agents: CreateRun answered a run that is not of this repository")
		form.Summary = ascagentui.Classify(errAscAgentsBadAnswer).Message
		ascAgentsRenderForm(ctx, env, &form, http.StatusBadGateway)
		return
	}
	started := "Run " + strconv.FormatUint(row.ID, 10) + " started."
	if len(resp.GetLowered()) > 0 {
		ctx.Flash.Warning(ascAgentsLoweredText(started+" A limit was above the policy cap.", resp.GetLowered()))
	} else {
		ctx.Flash.Success(started)
	}
	ctx.Redirect(row.Link)
}

// ---------------------------------------------------------------------------------------------- the run page

// ascAgentChangeRow is one change of the result stack.
type ascAgentChangeRow = ascagentui.ChangeLink

// AscAgentsRun serves `GET /{owner}/{repo}/asc/agents/runs/{id}`.
func AscAgentsRun(ctx *context.Context) {
	env, ok := ascAgentsGate(ctx, ascAgentsOpts{level: ascAgentsRead})
	if !ok {
		return
	}
	id, ok := ascAgentsParseID(ctx.Params("id"))
	if !ok {
		ctx.NotFound("asc agent run", nil)
		return
	}
	ctx.Data["Title"] = "Agent run " + strconv.FormatUint(id, 10)
	ascAgentsNav(ctx, "runs")
	conn, err := ascRPCConn()
	if err != nil {
		log.Error("asc agents: no connection to asc serve: %v", err)
		ascAgentsFail(ctx, tplAscAgentsRun, "connection", err)
		return
	}
	callCtx, cancel := ascAgentsReadCtx(ctx)
	defer cancel()
	api := ascv1.NewAgentsClient(conn)
	resp, err := api.GetRun(callCtx, &ascv1.GetAgentRunRequest{Repo: env.ref(), Id: id})
	if err != nil {
		ascAgentsFail(ctx, tplAscAgentsRun, "GetRun", err)
		return
	}
	run := resp.GetRun()
	row, ok := ascAgentRunOf(run, env.name, env.runsLink(), id)
	ascAgentsReveal(ctx, &row, run)
	see := ascAgentsMaySeeRun(ctx, run.GetCreatedById())
	ctx.Data["AscAgentRunSee"] = see
	if !ok {
		log.Error("asc agents: GetRun answered a run that is not the one asked for")
		ascAgentsFail(ctx, tplAscAgentsRun, "GetRun", errAscAgentsBadAnswer)
		return
	}
	canPulls := ctx.Repo.CanRead(unit.TypePullRequests)
	changes, err := ascagentui.ChangeLinks(ctx, ctx.Repo.Repository, env.link, run.GetChanges(), canPulls)
	if err != nil {
		log.Error("asc agents: resolving the changes of run %d: %v", id, err)
		changes, _ = ascagentui.ChangeLinks(ctx, ctx.Repo.Repository, env.link, run.GetChanges(), false)
	}

	// The first render holds the newest events; the script continues from the last sequence number.
	batch := &ascAgentFeedBatch{ended: !row.State.Active}
	if see { // the log holds what the agent read and wrote: its creator and the administrators only
		batch, err = ascAgentsCollect(callCtx, api, env.ref(), id, 0, ascAgentsBacklogWait, ascAgentsBacklogDrain, ascAgentsBacklogMaxEvents, ascAgentsBacklogMaxBytes)
	}
	if err != nil {
		ascAgentsFail(ctx, tplAscAgentsRun, "WatchRun", err)
		return
	}
	events := batch.events
	hidden := 0
	if len(events) > ascAgentsShownEvents {
		hidden = len(events) - ascAgentsShownEvents
		events = events[hidden:]
	}
	var last uint64
	if n := len(batch.events); n > 0 {
		last = batch.events[n-1].GetSeq()
	}
	urls := ascAgentEventChangeURLs(ctx, ctx.Repo.Repository, env.link, events, canPulls)

	ctx.Data["AscAgentRun"] = row
	ctx.Data["AscAgentRunKey"] = ascagentui.CleanLine(run.GetAiKeyId(), 40)
	ctx.Data["AscAgentRunTarget"] = ascagentui.CleanLine(run.GetTarget(), 255)
	ctx.Data["AscAgentRunStarted"] = ascagentui.Time(run.GetStarted())
	ctx.Data["AscAgentRunWorkflow"] = run.GetWorkflowRunId()
	ctx.Data["AscAgentRunWaiting"] = run.GetState() == ascv1.AgentRunState_AGENT_RUN_STATE_WAITING_INPUT
	ctx.Data["AscAgentMeters"] = ascagentui.Meters(run.GetLimits(), run.GetUsage())
	ctx.Data["AscAgentChanges"] = changes
	ctx.Data["AscAgentEvents"] = ascAgentEventRows(events, urls)
	ctx.Data["AscAgentEventsHidden"] = hidden
	ctx.Data["AscAgentLastSeq"] = last
	ctx.Data["AscAgentFeedDone"] = batch.ended && !row.State.Active
	ctx.Data["AscAgentLive"] = see
	ctx.Data["AscAgentPolicy"] = ascAgentPolicyOf(callCtx, env.ref())
	ctx.Data["AscAgentCanAct"] = ctx.Repo.CanWrite(unit.TypeCode)
	ctx.Data["AscAgentCanOpen"] = ascAgentsCanOpenPulls(ctx, changes)
	ctx.Data["AscAgentsMaxInput"] = ascagentui.MaxInputBytes
	ctx.Data["AscAgentsPollURL"] = env.runsLink() + "/runs/" + strconv.FormatUint(id, 10) + "/events"
	if see {
		ctx.Data["AscAgentRunTask"] = ascagentui.CleanText(run.GetTask(), 1200)
	}
	ctx.Data["AscAgentRunTaskHidden"] = !see && run.GetTask() != ""
	ctx.Data["AscAgentRunPaused"] = run.GetPaused()
	ctx.Data["AscAgentRunTokensLimit"] = run.GetLimits().GetMaxTokens()
	ctx.Data["AscAgentRunCostLimit"] = ascAgentsUSDField(run.GetLimits().GetMaxCostMicroUsd())
	ctx.Data["AscAgentRunMinutesLimit"] = run.GetLimits().GetMaxRuntimeSeconds() / 60
	ctx.Data["AscAgentRunChangesLimit"] = run.GetLimits().GetMaxChanges()
	ctx.Data["AscAgentRunSummaryTokens"] = ascagentui.FormatCount(run.GetUsage().GetTokens())
	ctx.Data["AscAgentRunSummaryCost"] = ascagentui.FormatUSD(run.GetUsage().GetCostMicroUsd())
	ctx.Data["AscAgentRunSummaryTime"] = ascagentui.FormatDuration(uint64(run.GetUsage().GetRuntimeSeconds()))
	ascAgentsLoadRunBoard(ctx, env, callCtx, conn, run, see)
	ascAgentsLoadRunToken(ctx, env, callCtx, conn, run)
	ctx.HTML(http.StatusOK, tplAscAgentsRun)
}

// ascAgentsLoadRunToken puts the token the run works with into ctx.Data["AscAgentRunToken"] for the "Access for this
// run" card: its id, state, expiry and the paths it may write. asc answers GetToken only to a repository administrator,
// so for anyone else the card shows the repository policy alone. A failed read leaves the card out.
func ascAgentsLoadRunToken(ctx *context.Context, env *ascAgentsEnv, callCtx gocontext.Context, conn grpc.ClientConnInterface, run *ascv1.AgentRun) {
	if !ctx.Repo.IsAdmin() || !ascagentui.ValidTokenID(run.GetTokenId()) {
		return
	}
	resp, err := ascv1.NewAgentTokensClient(conn).GetToken(callCtx, &ascv1.GetAgentTokenRequest{Repo: env.ref(), Id: run.GetTokenId()})
	if err != nil {
		log.Trace("asc agents: token of run %d: %v", run.GetId(), err)
		return
	}
	if row, ok := ascAgentTokenOf(resp.GetToken(), env.name); ok {
		ctx.Data["AscAgentRunToken"] = row
	}
}

// ascAgentFeedJSON is the answer of a poll.
type ascAgentFeedJSON struct {
	State   string             `json:"state"`
	Label   string             `json:"label"`
	Tone    string             `json:"tone"`
	Active  bool               `json:"active"`
	Waiting bool               `json:"waiting"`
	Title   string             `json:"title"`
	Meters  []ascagentui.Meter `json:"meters"`
	Changes int                `json:"changes"`
	Failure string             `json:"failure,omitempty"`
	Events  []ascAgentEvent    `json:"events"`
	Last    uint64             `json:"last"`
	Done    bool               `json:"done"`
}

// AscAgentsEvents serves `GET /{owner}/{repo}/asc/agents/runs/{id}/events?after=N`.
func AscAgentsEvents(ctx *context.Context) {
	env, ok := ascAgentsGate(ctx, ascAgentsOpts{level: ascAgentsRead})
	if !ok {
		return
	}
	id, ok := ascAgentsParseID(ctx.Params("id"))
	if !ok {
		ctx.NotFound("asc agent run", nil)
		return
	}
	after := uint64(0)
	if s := ctx.FormString("after"); s != "" {
		n, err := strconv.ParseUint(s, 10, 53)
		if err != nil || len(s) > 16 {
			ctx.JSON(http.StatusBadRequest, map[string]string{"error": "after is not a number"})
			return
		}
		after = n
	}
	ascAgentsNoStore(ctx)
	conn, err := ascRPCConn()
	if err != nil {
		log.Error("asc agents: no connection to asc serve: %v", err)
		ctx.JSON(http.StatusBadGateway, map[string]string{"error": ascagentui.Classify(err).Message})
		return
	}
	api := ascv1.NewAgentsClient(conn)
	// The poll may hold the request open, so it gets its own, longer deadline.
	callCtx, cancel := gocontext.WithTimeout(ascclient.WithSystem(ctx.Req.Context()), ascAgentsPollWait+ascAgentsCallTimeout)
	defer cancel()
	resp, err := api.GetRun(callCtx, &ascv1.GetAgentRunRequest{Repo: env.ref(), Id: id})
	if err != nil {
		ascAgentsFeedFail(ctx, "GetRun", err)
		return
	}
	run := resp.GetRun()
	row, ok := ascAgentRunOf(run, env.name, env.runsLink(), id)
	if !ok {
		log.Error("asc agents: GetRun answered a run that is not the one asked for")
		ctx.JSON(http.StatusBadGateway, map[string]string{"error": ascagentui.Classify(errAscAgentsBadAnswer).Message})
		return
	}
	wait := ascAgentsPollWait
	if !row.State.Active {
		wait = time.Second // a finished run only has its tail left
	}
	batch := &ascAgentFeedBatch{ended: !row.State.Active}
	if ascAgentsMaySeeRun(ctx, run.GetCreatedById()) {
		batch, err = ascAgentsCollect(callCtx, api, env.ref(), id, after, wait, ascAgentsDrainWait, ascAgentsPollMaxEvents, ascAgentsPollMaxBytes)
	}
	if err != nil {
		ascAgentsFeedFail(ctx, "WatchRun", err)
		return
	}
	// State and usage are read again after the wait, so the answer shows the run as of its last event.
	if len(batch.events) > 0 || row.State.Active {
		if again, err := api.GetRun(callCtx, &ascv1.GetAgentRunRequest{Repo: env.ref(), Id: id}); err == nil {
			if r2, ok := ascAgentRunOf(again.GetRun(), env.name, env.runsLink(), id); ok {
				run, row = again.GetRun(), r2
			}
		}
	}
	last := after
	if n := len(batch.events); n > 0 {
		last = batch.events[n-1].GetSeq()
	}
	canPulls := ctx.Repo.CanRead(unit.TypePullRequests)
	urls := ascAgentEventChangeURLs(ctx, ctx.Repo.Repository, env.link, batch.events, canPulls)
	out := ascAgentFeedJSON{
		State:   row.State.Slug,
		Label:   row.State.Label,
		Tone:    row.State.Tone,
		Active:  row.State.Active,
		Waiting: run.GetState() == ascv1.AgentRunState_AGENT_RUN_STATE_WAITING_INPUT,
		Title:   row.Title,
		Meters:  ascagentui.Meters(run.GetLimits(), run.GetUsage()),
		Changes: row.Changes,
		Failure: row.Failure,
		Events:  ascAgentEventRows(batch.events, urls),
		Last:    last,
		Done:    batch.ended && !row.State.Active,
	}
	ctx.JSON(http.StatusOK, out)
}

func ascAgentsFeedFail(ctx *context.Context, what string, err error) {
	f := ascagentui.Classify(err)
	if f == nil || ctx.Req.Context().Err() != nil {
		return
	}
	if f.Unavailable {
		log.Error("asc agents: %s failed: %s", what, ascagentuiLogLine(f))
	}
	status := f.HTTP
	if f.Disabled {
		status = http.StatusConflict
	}
	ctx.JSON(status, map[string]string{"error": f.Message})
}

// AscAgentsCancel serves `POST /{owner}/{repo}/asc/agents/runs/{id}/cancel`.
func AscAgentsCancel(ctx *context.Context) {
	env, ok := ascAgentsGate(ctx, ascAgentsOpts{level: ascAgentsWrite})
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
	if ctx.FormString("next") == "list" {
		back = env.runsLink()
	}
	reason, ok := ascagentui.CheckLine(ctx.FormString("reason"), ascagentui.MaxReasonBytes, true)
	if !ok {
		ctx.Flash.Error("The reason is at most 256 characters on one line.")
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
	resp, err := ascv1.NewAgentsClient(conn).CancelRun(callCtx, &ascv1.CancelAgentRunRequest{Repo: env.ref(), Id: id, Reason: reason})
	if err != nil {
		ascAgentsFlashFail(ctx, back, "CancelRun", err)
		return
	}
	if _, ok := ascAgentRunOf(resp.GetRun(), env.name, env.runsLink(), id); !ok {
		ascAgentsFlashFail(ctx, back, "CancelRun", errAscAgentsBadAnswer)
		return
	}
	ctx.Flash.Success("The run was stopped.")
	ctx.Redirect(back)
}

// AscAgentsInput serves `POST /{owner}/{repo}/asc/agents/runs/{id}/input`.
func AscAgentsInput(ctx *context.Context) {
	env, ok := ascAgentsGate(ctx, ascAgentsOpts{level: ascAgentsWrite})
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
	text, ok := ascagentui.CheckFreeText(ctx.FormString("text"), ascagentui.MaxInputBytes)
	if !ok {
		ctx.Flash.Error("Write a message of 1 to 16,384 bytes.")
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
	resp, err := ascv1.NewAgentsClient(conn).SendInput(callCtx, &ascv1.SendAgentInputRequest{Repo: env.ref(), Id: id, Text: text})
	if err != nil {
		ascAgentsFlashFail(ctx, back, "SendInput", err)
		return
	}
	if _, ok := ascAgentRunOf(resp.GetRun(), env.name, env.runsLink(), id); !ok {
		ascAgentsFlashFail(ctx, back, "SendInput", errAscAgentsBadAnswer)
		return
	}
	ctx.Flash.Success("Sent to the agent.")
	ctx.Redirect(back)
}

// ascAgentsUSDField writes micro-USD as the dollars a form field takes: 5000000 is "5", 1500000 is "1.5". 0 is "".
func ascAgentsUSDField(micro uint64) string {
	if micro == 0 {
		return ""
	}
	return strconv.FormatFloat(float64(micro)/1e6, 'f', -1, 64)
}
