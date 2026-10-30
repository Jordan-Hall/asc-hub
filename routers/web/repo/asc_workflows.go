// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	gocontext "context"
	"encoding/hex"
	"net/http"
	"net/url"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"forgejo.org/models/unit"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/base"
	"forgejo.org/modules/log"
	"forgejo.org/modules/util"
	"forgejo.org/services/ascagentui"
	"forgejo.org/services/asctrust"
	"forgejo.org/services/context"

	"google.golang.org/grpc"
)

// Workflows: agent work that starts on an event and runs steps in order (asc `Workflows` service). A definition is
// the file `.asc/workflows/<name>.toml` on trunk, so a change to a workflow is reviewed like code; the server runs
// the version on the trunk tip and keeps only whether a workflow is enabled and its runs.
//
//	GET  /{owner}/{repo}/asc/workflows                        definitions, enable state, the latest runs
//	GET  /{owner}/{repo}/asc/workflows/runs                   run history (filters: workflow, state, change)
//	GET  /{owner}/{repo}/asc/workflows/runs/{id}              one run: steps, approvals, per-step logs
//	GET  /{owner}/{repo}/asc/workflows/runs/{id}/state        the poll of the live run page (JSON)
//	POST /{owner}/{repo}/asc/workflows/runs/{id}/cancel       stop a run
//	POST /{owner}/{repo}/asc/workflows/runs/{id}/decide       approve or reject a waiting approval step
//	POST /{owner}/{repo}/asc/workflows/runs/{id}/rerun        start the workflow again with the run's inputs
//	GET  /{owner}/{repo}/asc/workflows/{name}                 one definition: triggers, inputs, steps, the file, a trigger form
//	POST /{owner}/{repo}/asc/workflows/{name}/trigger         start a run by hand
//	POST /{owner}/{repo}/asc/workflows/{name}/enable          switch the workflow on or off (maintainers; asc decides)
//
// The reading pages are open to everyone who can read the code and is signed in; agents are optional, so they
// do not need the agent features switch. Everything that starts, stops, decides or switches needs the switch
// and code write access, and asc applies its own rules (maintainer, named approver, budgets) to the person's
// one-use token. The per-step logs and prompts are agent parts and need the switch. Text from asc (descriptions,
// prompts, errors, log lines) is cleaned and escaped; the file text is shown as text.

const (
	tplAscWorkflows    base.TplName = "repo/asc_workflows"
	tplAscWorkflowsDef base.TplName = "repo/asc_workflows_def"
	tplAscWorkflowsRun base.TplName = "repo/asc_workflows_run"
	tplAscWorkflowsAll base.TplName = "repo/asc_workflows_runs"

	ascWorkflowsPageSize   = 30
	ascWorkflowsScanPages  = 5
	ascWorkflowsScanSize   = 100
	ascWorkflowsMaxSteps   = 32
	ascWorkflowsMaxLogRuns = 8
	ascWorkflowsLogEvents  = 60
	ascWorkflowsLogShown   = 25
	ascWorkflowsMaxInputs  = 32
	ascWorkflowsMaxText    = 64 << 10
)

func (e *ascAgentsEnv) workflowsLink() string { return e.link + "/asc/workflows" }

func ascWorkflowsNav(ctx *context.Context, tab string) {
	ctx.Data["PageIsAscAgentArea"] = true
	ctx.Data["AscAgentsTab"] = tab
}

// ---------------------------------------------------------------------------------------------- view models

type ascWorkflowTrigger struct {
	Label    string
	Branches []string
	Paths    []string
	Cron     string
	Agent    string
}

type ascWorkflowInput struct {
	Name        string
	Description string
	Required    bool
	Default     string
}

type ascWorkflowStep struct {
	ID        string
	Kind      string
	IsAgent   bool
	Needs     []string
	Agent     string
	Prompt    string
	Paths     []string
	Approvers []string
	Checks    []string
	Timeout   string
	Continue  bool
	Meters    []ascagentui.Meter
	// Command is the argument list of a command step; Secrets are the repository secrets it reads, by environment variable.
	Command []string
	Secrets []ascWorkflowSecretUse
	// HasState is true when the latest run of the workflow ran this step; State is how it went.
	HasState bool
	State    ascagentui.State
}

// ascWorkflowSecretUse is one secret a command step reads. Set is filled when the viewer may list the secrets of the repository.
type ascWorkflowSecretUse struct {
	Env     string
	Name    string
	Checked bool
	Set     bool
}

// ascWorkflowSecretUseRow is a secret of the repository with the workflows that read it, for the side card of the list.
type ascWorkflowSecretUseRow struct {
	Name      string
	Workflows []string
}

// ascWorkflowSecretName is one secret of the repository that the workflow reads.
type ascWorkflowSecretName struct {
	Name    string
	Envs    []string
	Steps   []string
	Checked bool
	Set     bool
}

type ascWorkflowDef struct {
	Name        string
	Path        string
	Description string
	Enabled     bool
	Valid       bool
	Error       string
	Source      string
	Triggers    []ascWorkflowTrigger
	Inputs      []ascWorkflowInput
	Steps       []ascWorkflowStep
	Meters      []ascagentui.Meter
	MaxParallel uint32
	Link        string
	HasAgent    bool
	// SecretsUsed are the repository secrets the command steps read, each once, with the steps and variables that read it. Set is
	// known only when the viewer may list the secrets of the repository (Checked).
	SecretsUsed []ascWorkflowSecretName
	// History is how the latest runs of the workflow went (at most 12, oldest first); LastRun is when the newest one started.
	History []ascagentui.State
	LastRun time.Time
}

func ascCleanList(in []string, max, maxBytes int) []string {
	var out []string
	for _, s := range in {
		if len(out) >= max {
			break
		}
		if c := ascagentui.CleanLine(s, maxBytes); c != "" {
			out = append(out, c)
		}
	}
	return out
}

func ascHexShort(b []byte) string {
	if len(b) == 0 || len(b) > 64 {
		return ""
	}
	h := hex.EncodeToString(b)
	if len(h) > 12 {
		h = h[:12]
	}
	return h
}

// ascWorkflowDefOf checks a definition that asc sent. A definition whose name is not a file stem is refused.
// showPrompts is the agent features switch: a prompt is a task for an agent.
func ascWorkflowDefOf(d *ascv1.WorkflowDefinition, workflowsLink string, showPrompts bool) (ascWorkflowDef, bool) {
	if d == nil || !ascagentui.ValidWorkflowName(d.GetName()) {
		return ascWorkflowDef{}, false
	}
	out := ascWorkflowDef{
		Name:        d.GetName(),
		Path:        ascagentui.CleanLine(d.GetPath(), 255),
		Description: ascagentui.CleanLine(d.GetDescription(), 500),
		Enabled:     d.GetEnabled(),
		Valid:       d.GetValid(),
		Source:      ascHexShort(d.GetSource()),
		MaxParallel: d.GetMaxParallelRuns(),
		Link:        workflowsLink + "/" + d.GetName(),
		Meters:      ascagentui.Meters(d.GetLimits(), nil),
	}
	if out.MaxParallel == 0 {
		out.MaxParallel = 1
	}
	if !out.Valid {
		out.Error = ascagentui.CleanLine(d.GetError(), 400)
		if out.Error == "" {
			out.Error = "The definition is not valid."
		}
	}
	for i, t := range d.GetTriggers() {
		if i >= 16 {
			break
		}
		out.Triggers = append(out.Triggers, ascWorkflowTrigger{
			Label:    ascagentui.TriggerLabel(t.GetKind()),
			Branches: ascCleanList(t.GetBranches(), 16, 128),
			Paths:    ascCleanList(t.GetPaths(), 16, ascagentui.MaxGlobBytes),
			Cron:     ascagentui.CleanLine(t.GetCron(), 64),
			Agent:    ascagentui.SafeName(t.GetAgent()),
		})
	}
	for i, in := range d.GetInputs() {
		if i >= ascWorkflowsMaxInputs {
			break
		}
		if !ascagentui.ValidInputName(in.GetName()) {
			continue
		}
		out.Inputs = append(out.Inputs, ascWorkflowInput{
			Name:        in.GetName(),
			Description: ascagentui.CleanLine(in.GetDescription(), 300),
			Required:    in.GetRequired(),
			Default:     ascagentui.CleanLine(in.GetDefaultValue(), 200),
		})
	}
	for i, s := range d.GetSteps() {
		if i >= ascWorkflowsMaxSteps {
			break
		}
		if !ascagentui.ValidStepID(s.GetId()) {
			continue
		}
		st := ascWorkflowStep{
			ID:        s.GetId(),
			Kind:      ascagentui.StepKind(s.GetKind()),
			IsAgent:   s.GetKind() == ascv1.WorkflowStepKind_WORKFLOW_STEP_KIND_AGENT,
			Agent:     ascagentui.SafeName(s.GetAgent()),
			Paths:     ascCleanList(s.GetPaths(), 16, ascagentui.MaxGlobBytes),
			Approvers: ascCleanList(s.GetApprovers(), 16, 80),
			Checks:    ascCleanList(s.GetChecks(), 16, 80),
			Continue:  s.GetContinueOnFailure(),
		}
		for _, n := range s.GetNeeds() {
			if ascagentui.ValidStepID(n) && len(st.Needs) < ascWorkflowsMaxSteps {
				st.Needs = append(st.Needs, n)
			}
		}
		st.Command = ascCleanList(s.GetCommand(), 32, 200)
		if sec := s.GetSecrets(); len(sec) > 0 {
			envs := make([]string, 0, len(sec))
			for e := range sec {
				envs = append(envs, e)
			}
			sort.Strings(envs)
			for _, e := range envs {
				if len(st.Secrets) >= 16 {
					break
				}
				st.Secrets = append(st.Secrets, ascWorkflowSecretUse{Env: ascagentui.CleanLine(e, 64), Name: ascagentui.CleanLine(sec[e], 64)})
			}
		}
		if t := s.GetTimeoutSeconds(); t > 0 {
			st.Timeout = ascagentui.FormatDuration(uint64(t))
		}
		if st.IsAgent {
			out.HasAgent = true
			st.Meters = ascagentui.Meters(s.GetLimits(), nil)
			if showPrompts {
				st.Prompt = ascagentui.CleanText(s.GetPrompt(), ascagentui.MaxInputBytes)
			}
		}
		out.Steps = append(out.Steps, st)
	}
	index := map[string]int{}
	for _, st := range out.Steps {
		for _, use := range st.Secrets {
			k, ok := index[use.Name]
			if !ok {
				k = len(out.SecretsUsed)
				index[use.Name] = k
				out.SecretsUsed = append(out.SecretsUsed, ascWorkflowSecretName{Name: use.Name})
			}
			u := &out.SecretsUsed[k]
			u.Envs = append(u.Envs, use.Env)
			if !slices.Contains(u.Steps, st.ID) {
				u.Steps = append(u.Steps, st.ID)
			}
		}
	}
	return out, true
}

type ascWorkflowStepRun struct {
	ID string
	// Private is true when the step is an agent step whose run the viewer may not read: its log and failure text are hidden.
	Private    bool
	State      ascagentui.State
	AgentRunID uint64
	AgentLink  string
	Started    time.Time
	Finished   time.Time
	DecidedBy  string
	Comment    string
	Error      string
	Waiting    bool // an approval step that waits for a decision
	Log        []ascAgentEvent
	LogHidden  int
}

type ascWorkflowRunRow struct {
	ID uint64
	// ErrorPrivate is true when the failure text of the run was left out because an agent step of it is private to others.
	ErrorPrivate bool
	Workflow     string
	State        ascagentui.State
	Trigger      string
	StartedBy    string
	Change       string
	ChangeText   string
	ChangeURL    string
	Source       string
	Created      time.Time
	Finished     time.Time
	Error        string
	Inputs       []ascWorkflowInputValue
	Steps        []ascWorkflowStepRun
	Meters       []ascagentui.Meter
	Link         string
	Done         int
}

type ascWorkflowInputValue struct{ Name, Value string }

// ascWorkflowRunOf checks a run that asc sent for repository `name`. wantID is 0 in a list.
func ascWorkflowRunOf(r *ascv1.WorkflowRun, name, workflowsLink, repoLink string, wantID uint64) (ascWorkflowRunRow, bool) {
	if r == nil || r.GetId() == 0 || r.GetRepo() != name || (wantID != 0 && r.GetId() != wantID) || !ascagentui.ValidWorkflowName(r.GetWorkflow()) {
		return ascWorkflowRunRow{}, false
	}
	row := ascWorkflowRunRow{
		ID:        r.GetId(),
		Workflow:  r.GetWorkflow(),
		State:     ascagentui.WorkflowRunState(r.GetState()),
		Trigger:   ascagentui.TriggerLabel(r.GetTrigger()),
		StartedBy: ascagentui.SafeName(r.GetStartedBy()),
		Source:    ascHexShort(r.GetSource()),
		Created:   ascagentui.Time(r.GetCreated()),
		Finished:  ascagentui.Time(r.GetFinished()),
		Error:     ascagentui.CleanLine(r.GetError(), 400),
		Link:      workflowsLink + "/runs/" + strconv.FormatUint(r.GetId(), 10),
	}
	for _, m := range ascagentui.Meters(nil, r.GetUsage()) {
		if !m.Idle {
			row.Meters = append(row.Meters, m)
		}
	}
	if text := ascagentui.ChangeText(r.GetChange()); text != "" {
		row.ChangeText, row.Change = text, ascagentui.ShortChange(text)
	}
	names := make([]string, 0, len(r.GetInputs()))
	for k := range r.GetInputs() {
		if ascagentui.ValidInputName(k) {
			names = append(names, k)
		}
	}
	sort.Strings(names)
	for _, k := range names {
		if len(row.Inputs) >= ascWorkflowsMaxInputs {
			break
		}
		row.Inputs = append(row.Inputs, ascWorkflowInputValue{k, ascagentui.CleanLine(r.GetInputs()[k], 300)})
	}
	for i, s := range r.GetSteps() {
		if i >= ascWorkflowsMaxSteps {
			break
		}
		if !ascagentui.ValidStepID(s.GetId()) {
			continue
		}
		sr := ascWorkflowStepRun{
			ID:         s.GetId(),
			State:      ascagentui.StepState(s.GetState()),
			AgentRunID: s.GetAgentRunId(),
			Started:    ascagentui.Time(s.GetStarted()),
			Finished:   ascagentui.Time(s.GetFinished()),
			DecidedBy:  ascagentui.SafeName(s.GetDecidedBy()),
			Comment:    ascagentui.CleanLine(s.GetComment(), ascagentui.MaxCommentBytes),
			Error:      ascagentui.CleanLine(s.GetError(), 400),
			Waiting:    s.GetState() == ascv1.WorkflowStepState_WORKFLOW_STEP_STATE_WAITING && s.GetAgentRunId() == 0,
		}
		if sr.AgentRunID != 0 {
			sr.AgentLink = repoLink + "/asc/agents/runs/" + strconv.FormatUint(sr.AgentRunID, 10)
		}
		if !sr.State.Active {
			row.Done++
		}
		row.Steps = append(row.Steps, sr)
	}
	return row, true
}

// ---------------------------------------------------------------------------------------------- the list

// AscWorkflows serves `GET /{owner}/{repo}/asc/workflows`.
func AscWorkflows(ctx *context.Context) {
	env, ok := ascAgentsGate(ctx, ascAgentsOpts{level: ascAgentsRead, noFeatures: true})
	if !ok {
		return
	}
	ctx.Data["Title"] = "Workflows"
	ascWorkflowsNav(ctx, "workflows")
	ctx.Data["CanChangeWorkflows"] = env.features && ctx.Repo.CanWrite(unit.TypeCode) && !ctx.Repo.Repository.IsArchived

	conn, err := ascRPCConn()
	if err != nil {
		log.Error("asc workflows: no connection to asc serve: %v", err)
		ascAgentsFail(ctx, tplAscWorkflows, "connection", err)
		return
	}
	callCtx, cancel := ascAgentsReadCtx(ctx)
	defer cancel()
	api := ascv1.NewWorkflowsClient(conn)
	defs, err := api.ListDefinitions(callCtx, &ascv1.ListWorkflowDefinitionsRequest{Repo: env.ref()})
	if err != nil {
		ascAgentsFail(ctx, tplAscWorkflows, "ListDefinitions", err)
		return
	}
	rows := make([]ascWorkflowDef, 0, len(defs.GetDefinitions()))
	skipped := 0
	for _, d := range defs.GetDefinitions() {
		row, ok := ascWorkflowDefOf(d, env.workflowsLink(), env.features)
		if !ok {
			skipped++ // a name the hub cannot link to (or "runs", the history page)
			continue
		}
		rows = append(rows, row)
	}
	// The state chips count every definition; the filter and the search narrow what is listed.
	counts := ascWorkflowCounts(rows)
	state := ctx.FormString("state")
	if state != "" && state != "on" && state != "off" && state != "invalid" {
		state = ""
	}
	q := ctx.FormTrim("q")
	if len(q) > 80 || strings.ContainsAny(q, "\x00\r\n") {
		q = ""
	}
	// The runs say how each workflow went lately and how many run now; they are a convenience, the page stands without them.
	var running, allowed uint32
	for _, r := range rows {
		if r.Valid && r.Enabled {
			allowed += r.MaxParallel
		}
	}
	if runs, err := api.ListRuns(callCtx, &ascv1.ListWorkflowRunsRequest{Repo: env.ref(), Limit: 100}); err == nil {
		byName := map[string][]ascWorkflowRunRow{}
		for _, r := range runs.GetRuns() { // newest first
			row, ok := ascWorkflowRunOf(r, env.name, env.workflowsLink(), env.link, 0)
			if !ok {
				continue
			}
			if row.State.Active {
				running++
			}
			if len(byName[row.Workflow]) < 12 {
				byName[row.Workflow] = append(byName[row.Workflow], row)
			}
		}
		for i := range rows {
			rs := byName[rows[i].Name]
			for j := len(rs) - 1; j >= 0; j-- {
				rows[i].History = append(rows[i].History, rs[j].State)
			}
			if len(rs) > 0 {
				rows[i].LastRun = rs[0].Created
			}
		}
		ctx.Data["AscWorkflowRunning"], ctx.Data["AscWorkflowAllowed"] = running, allowed
		ctx.Data["AscWorkflowCapacityPct"] = ascagentui.Percent(uint64(running), uint64(max(allowed, 1)))
	}
	// Which secrets the workflows read, once each.
	var used []ascWorkflowSecretUseRow
	at := map[string]int{}
	for _, r := range rows {
		for _, u := range r.SecretsUsed {
			k, ok := at[u.Name]
			if !ok {
				k = len(used)
				at[u.Name] = k
				used = append(used, ascWorkflowSecretUseRow{Name: u.Name})
			}
			used[k].Workflows = append(used[k].Workflows, r.Name)
		}
	}
	ctx.Data["AscWorkflowSecretsUsed"] = used
	rows = ascWorkflowFilter(rows, state, q)
	ctx.Data["AscWorkflowCountAll"], ctx.Data["AscWorkflowCountOn"], ctx.Data["AscWorkflowCountOff"], ctx.Data["AscWorkflowCountInvalid"] = counts.All, counts.On, counts.Off, counts.Invalid
	ctx.Data["AscWorkflowState"] = state
	ctx.Data["AscWorkflowQ"] = q
	ctx.Data["AscWorkflowDefs"] = rows
	ctx.Data["AscWorkflowSkipped"] = skipped

	// The latest runs are a convenience; the page stands without them.
	if runs, err := api.ListRuns(callCtx, &ascv1.ListWorkflowRunsRequest{Repo: env.ref(), Limit: 8}); err == nil {
		var recent []ascWorkflowRunRow
		for _, r := range runs.GetRuns() {
			if row, ok := ascWorkflowRunOf(r, env.name, env.workflowsLink(), env.link, 0); ok {
				recent = append(recent, row)
			}
		}
		ctx.Data["AscWorkflowRecent"] = recent
	}
	ctx.HTML(http.StatusOK, tplAscWorkflows)
}

// ---------------------------------------------------------------------------------------------- one definition

// ascWorkflowTriggerForm is the manual trigger form as typed.
type ascWorkflowTriggerForm struct {
	Values  map[string]string
	Change  string
	OpID    string
	Errors  map[string]string
	Summary string
}

// AscWorkflowDef serves `GET /{owner}/{repo}/asc/workflows/{name}`.
func AscWorkflowDef(ctx *context.Context) {
	env, ok := ascAgentsGate(ctx, ascAgentsOpts{level: ascAgentsRead, noFeatures: true})
	if !ok {
		return
	}
	ascWorkflowDefPage(ctx, env, nil, http.StatusOK)
}

func ascWorkflowDefPage(ctx *context.Context, env *ascAgentsEnv, form *ascWorkflowTriggerForm, status int) {
	name := ctx.Params("name")
	if !ascagentui.ValidWorkflowName(name) {
		ctx.NotFound("asc workflow", nil)
		return
	}
	ctx.Data["Title"] = "Workflow " + name
	ascWorkflowsNav(ctx, "workflows")
	ctx.Data["CanChangeWorkflows"] = env.features && ctx.Repo.CanWrite(unit.TypeCode) && !ctx.Repo.Repository.IsArchived

	conn, err := ascRPCConn()
	if err != nil {
		log.Error("asc workflows: no connection to asc serve: %v", err)
		ascAgentsFail(ctx, tplAscWorkflowsDef, "connection", err)
		return
	}
	callCtx, cancel := ascAgentsReadCtx(ctx)
	defer cancel()
	api := ascv1.NewWorkflowsClient(conn)
	resp, err := api.GetDefinition(callCtx, &ascv1.GetWorkflowDefinitionRequest{Repo: env.ref(), Name: name})
	if err != nil {
		ascAgentsFail(ctx, tplAscWorkflowsDef, "GetDefinition", err)
		return
	}
	def, ok := ascWorkflowDefOf(resp.GetDefinition(), env.workflowsLink(), env.features)
	if !ok || def.Name != name {
		log.Error("asc workflows: GetDefinition answered a definition that is not the one asked for")
		ascAgentsFail(ctx, tplAscWorkflowsDef, "GetDefinition", errAscAgentsBadAnswer)
		return
	}
	ctx.Data["AscWorkflowText"] = ascagentui.CleanText(string(resp.GetText()), ascWorkflowsMaxText)
	if def.Path != "" {
		// "Edit as a change": the file in the editor, on the new-branch choice, so the edit is a pull request and not a commit to trunk.
		ctx.Data["AscWorkflowEditURL"] = ctx.Repo.RepoLink + "/_edit/" + util.PathEscapeSegments(ctx.Repo.Repository.DefaultBranch) + "/" + util.PathEscapeSegments(def.Path) + "?commit_choice=commit-to-new-branch"
	}

	if form == nil {
		form = &ascWorkflowTriggerForm{Values: map[string]string{}, OpID: ascAgentsNewOpID()}
		for _, in := range def.Inputs {
			form.Values[in.Name] = in.Default
		}
	}
	ctx.Data["AscWorkflowForm"] = form

	if runs, err := api.ListRuns(callCtx, &ascv1.ListWorkflowRunsRequest{Repo: env.ref(), Name: name, Limit: 10}); err == nil {
		var rows []ascWorkflowRunRow
		for _, r := range runs.GetRuns() {
			if row, ok := ascWorkflowRunOf(r, env.name, env.workflowsLink(), env.link, 0); ok {
				rows = append(rows, row)
			}
		}
		ctx.Data["AscWorkflowRuns"] = rows
		if len(rows) > 0 {
			// The newest run says how each step went last time.
			ran := map[string]ascagentui.State{}
			for _, sr := range rows[0].Steps {
				ran[sr.ID] = sr.State
			}
			for i := range def.Steps {
				if st, ok := ran[def.Steps[i].ID]; ok {
					def.Steps[i].HasState, def.Steps[i].State = true, st
				}
			}
		}
	}
	ascWorkflowSecretsCard(ctx, conn, env, &def)
	ctx.Data["AscWorkflowDef"] = def
	ctx.HTML(status, tplAscWorkflowsDef)
}

// AscWorkflowTrigger serves `POST /{owner}/{repo}/asc/workflows/{name}/trigger`.
func AscWorkflowTrigger(ctx *context.Context) {
	env, ok := ascAgentsGate(ctx, ascAgentsOpts{level: ascAgentsWrite, notArchived: true})
	if !ok {
		return
	}
	name := ctx.Params("name")
	if !ascagentui.ValidWorkflowName(name) {
		ctx.NotFound("asc workflow", nil)
		return
	}
	ascAgentsLimitBody(ctx)
	conn, err := ascRPCConn()
	if err != nil {
		ascAgentsFlashFail(ctx, env.workflowsLink()+"/"+name, "connection", err)
		return
	}
	api := ascv1.NewWorkflowsClient(conn)

	// The inputs the form may send are the ones the definition names; anything else is ignored, never forwarded.
	readCtx, cancelRead := ascAgentsReadCtx(ctx)
	got, err := api.GetDefinition(readCtx, &ascv1.GetWorkflowDefinitionRequest{Repo: env.ref(), Name: name})
	cancelRead()
	if err != nil {
		ascAgentsFlashFail(ctx, env.workflowsLink()+"/"+name, "GetDefinition", err)
		return
	}
	def, ok := ascWorkflowDefOf(got.GetDefinition(), env.workflowsLink(), false)
	if !ok || def.Name != name {
		ascAgentsFlashFail(ctx, env.workflowsLink()+"/"+name, "GetDefinition", errAscAgentsBadAnswer)
		return
	}

	form := &ascWorkflowTriggerForm{Values: map[string]string{}, Errors: map[string]string{}, OpID: ctx.FormTrim("op"), Change: ctx.FormTrim("change")}
	if form.OpID == "" {
		form.OpID = ascAgentsNewOpID()
	}
	req := &ascv1.TriggerWorkflowRequest{Repo: env.ref(), Name: name, Inputs: map[string]string{}}
	for _, in := range def.Inputs {
		raw := ctx.FormString("in_" + in.Name)
		form.Values[in.Name] = raw
		if raw == "" {
			if in.Required && in.Default == "" {
				form.Errors[in.Name] = "This input is required."
			}
			continue
		}
		v, ok := ascagentui.CheckFreeText(raw, ascagentui.MaxInputValue)
		if !ok {
			form.Errors[in.Name] = "Use text of at most 4,096 bytes."
			continue
		}
		req.Inputs[in.Name] = v
	}
	if form.Change != "" {
		if b := ascagentui.ChangeBytes(form.Change); b != nil {
			req.Change = b
		} else {
			form.Errors["change"] = "A change id is 32 letters, k to z."
		}
	}
	if len(form.Errors) > 0 {
		form.Summary = "Some fields need another look."
		ascWorkflowDefPage(ctx, env, form, http.StatusBadRequest)
		return
	}
	callCtx, done, ok := ascAgentsWriteCtx(ctx, form.OpID)
	if !ok {
		return
	}
	resp, err := api.Trigger(callCtx, req)
	done()
	if err != nil {
		f := ascagentui.Classify(err)
		if f == nil || ctx.Req.Context().Err() != nil {
			return
		}
		if f.Unavailable {
			log.Error("asc workflows: Trigger failed: %s", ascagentuiLogLine(f))
		}
		form.Summary = f.Message
		for _, d := range f.Details {
			form.Summary += " " + d + "."
		}
		ascWorkflowDefPage(ctx, env, form, f.HTTP)
		return
	}
	row, ok := ascWorkflowRunOf(resp.GetRun(), env.name, env.workflowsLink(), env.link, 0)
	if !ok || row.Workflow != name {
		ascAgentsFlashFail(ctx, env.workflowsLink()+"/"+name, "Trigger", errAscAgentsBadAnswer)
		return
	}
	ctx.Flash.Success("Workflow run " + strconv.FormatUint(row.ID, 10) + " started.")
	ctx.Redirect(row.Link)
}

// AscWorkflowEnable serves `POST /{owner}/{repo}/asc/workflows/{name}/enable` with enabled=1 or enabled=0.
func AscWorkflowEnable(ctx *context.Context) {
	env, ok := ascAgentsGate(ctx, ascAgentsOpts{level: ascAgentsWrite, notArchived: true})
	if !ok {
		return
	}
	name := ctx.Params("name")
	if !ascagentui.ValidWorkflowName(name) {
		ctx.NotFound("asc workflow", nil)
		return
	}
	ascAgentsLimitBody(ctx)
	back := env.workflowsLink() + "/" + name
	if ctx.FormString("next") == "list" {
		back = env.workflowsLink()
	}
	var enabled bool
	switch ctx.FormString("enabled") {
	case "1":
		enabled = true
	case "0":
	default:
		ctx.Error(http.StatusBadRequest)
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
	resp, err := ascv1.NewWorkflowsClient(conn).SetEnabled(callCtx, &ascv1.SetWorkflowEnabledRequest{Repo: env.ref(), Name: name, Enabled: enabled})
	if err != nil {
		ascAgentsFlashFail(ctx, back, "SetEnabled", err)
		return
	}
	if def, ok := ascWorkflowDefOf(resp.GetDefinition(), env.workflowsLink(), false); !ok || def.Name != name {
		ascAgentsFlashFail(ctx, back, "SetEnabled", errAscAgentsBadAnswer)
		return
	}
	if enabled {
		ctx.Flash.Success("The workflow is on.")
	} else {
		ctx.Flash.Success("The workflow is off.")
	}
	ctx.Redirect(back)
}

// ---------------------------------------------------------------------------------------------- run history

// AscWorkflowRuns serves `GET /{owner}/{repo}/asc/workflows/runs`. The change filter is done here, because the
// service lists by workflow and state only: the latest runs (at most 500) are scanned and the page says so.
func AscWorkflowRuns(ctx *context.Context) {
	env, ok := ascAgentsGate(ctx, ascAgentsOpts{level: ascAgentsRead, noFeatures: true})
	if !ok {
		return
	}
	ctx.Data["Title"] = "Workflow runs"
	ascWorkflowsNav(ctx, "workflows")

	name := ctx.FormTrim("workflow")
	stateSlug := ctx.FormString("state")
	states, valid := ascagentui.WorkflowRunStateFilter(stateSlug)
	change := ctx.FormTrim("change")
	token, tokenOK := ascAgentsPageToken(ctx.FormString("page"))
	var changeBytes []byte
	if change != "" {
		changeBytes = ascagentui.ChangeBytes(change)
	}
	if !valid || !tokenOK || (name != "" && !ascagentui.ValidWorkflowName(name)) || (change != "" && changeBytes == nil) {
		ctx.Data["AscAgentsFailure"] = ascagentui.Classify(ascInvalidArgument())
		ctx.HTML(http.StatusBadRequest, tplAscWorkflowsAll)
		return
	}
	ctx.Data["AscWorkflowFilterName"] = name
	ctx.Data["AscWorkflowFilterState"] = stateSlug
	ctx.Data["AscWorkflowFilterChange"] = change

	conn, err := ascRPCConn()
	if err != nil {
		log.Error("asc workflows: no connection to asc serve: %v", err)
		ascAgentsFail(ctx, tplAscWorkflowsAll, "connection", err)
		return
	}
	callCtx, cancel := ascAgentsReadCtx(ctx)
	defer cancel()
	api := ascv1.NewWorkflowsClient(conn)

	var rows []ascWorkflowRunRow
	next := ""
	scanned, scanCut := 0, false
	if changeBytes == nil {
		resp, err := api.ListRuns(callCtx, &ascv1.ListWorkflowRunsRequest{Repo: env.ref(), Name: name, States: states, Limit: ascWorkflowsPageSize, PageToken: token})
		if err != nil {
			ascAgentsFail(ctx, tplAscWorkflowsAll, "ListRuns", err)
			return
		}
		for _, r := range resp.GetRuns() {
			row, ok := ascWorkflowRunOf(r, env.name, env.workflowsLink(), env.link, 0)
			if !ok {
				ascAgentsFail(ctx, tplAscWorkflowsAll, "ListRuns", errAscAgentsBadAnswer)
				return
			}
			rows = append(rows, row)
		}
		next = resp.GetNextPageToken()
	} else {
		pageTok := ""
		for page := 0; page < ascWorkflowsScanPages; page++ {
			resp, err := api.ListRuns(callCtx, &ascv1.ListWorkflowRunsRequest{Repo: env.ref(), Name: name, States: states, Limit: ascWorkflowsScanSize, PageToken: pageTok})
			if err != nil {
				ascAgentsFail(ctx, tplAscWorkflowsAll, "ListRuns", err)
				return
			}
			for _, r := range resp.GetRuns() {
				row, ok := ascWorkflowRunOf(r, env.name, env.workflowsLink(), env.link, 0)
				if !ok {
					ascAgentsFail(ctx, tplAscWorkflowsAll, "ListRuns", errAscAgentsBadAnswer)
					return
				}
				scanned++
				if row.ChangeText == change {
					rows = append(rows, row)
				}
			}
			pageTok = resp.GetNextPageToken()
			if pageTok == "" {
				break
			}
			if _, ok := ascAgentsPageToken(pageTok); !ok {
				ascAgentsFail(ctx, tplAscWorkflowsAll, "ListRuns", errAscAgentsBadAnswer)
				return
			}
		}
		scanCut = pageTok != ""
	}
	if t, ok := ascAgentsPageToken(next); ok && t != "" {
		q := url.Values{}
		if name != "" {
			q.Set("workflow", name)
		}
		if stateSlug != "" {
			q.Set("state", stateSlug)
		}
		q.Set("page", t)
		ctx.Data["AscWorkflowNext"] = env.workflowsLink() + "/runs?" + q.Encode()
	}
	ctx.Data["AscWorkflowRuns"] = rows
	ctx.Data["AscWorkflowScanned"] = scanned
	ctx.Data["AscWorkflowScanCut"] = scanCut
	ctx.Data["AscWorkflowStateFilters"] = ascWorkflowStateFilters
	ctx.HTML(http.StatusOK, tplAscWorkflowsAll)
}

var ascWorkflowStateFilters = []ascagentui.State{
	{Slug: "active", Label: "Active"},
	{Slug: "waiting", Label: "Waiting for approval"},
	{Slug: "succeeded", Label: "Succeeded"},
	{Slug: "failed", Label: "Failed"},
	{Slug: "cancelled", Label: "Cancelled"},
}

// ---------------------------------------------------------------------------------------------- one run

// AscWorkflowRun serves `GET /{owner}/{repo}/asc/workflows/runs/{id}`.
func AscWorkflowRun(ctx *context.Context) {
	env, ok := ascAgentsGate(ctx, ascAgentsOpts{level: ascAgentsRead, noFeatures: true})
	if !ok {
		return
	}
	id, ok := ascAgentsParseID(ctx.Params("id"))
	if !ok {
		ctx.NotFound("asc workflow run", nil)
		return
	}
	ctx.Data["Title"] = "Workflow run " + strconv.FormatUint(id, 10)
	ascWorkflowsNav(ctx, "workflows")
	conn, err := ascRPCConn()
	if err != nil {
		log.Error("asc workflows: no connection to asc serve: %v", err)
		ascAgentsFail(ctx, tplAscWorkflowsRun, "connection", err)
		return
	}
	callCtx, cancel := ascAgentsReadCtx(ctx)
	defer cancel()
	resp, err := ascv1.NewWorkflowsClient(conn).GetRun(callCtx, &ascv1.GetWorkflowRunRequest{Repo: env.ref(), Id: id})
	if err != nil {
		ascAgentsFail(ctx, tplAscWorkflowsRun, "GetRun", err)
		return
	}
	row, ok := ascWorkflowRunOf(resp.GetRun(), env.name, env.workflowsLink(), env.link, id)
	if !ok {
		log.Error("asc workflows: GetRun answered a run that is not the one asked for")
		ascAgentsFail(ctx, tplAscWorkflowsRun, "GetRun", errAscAgentsBadAnswer)
		return
	}
	canPulls := ctx.Repo.CanRead(unit.TypePullRequests)
	if b := resp.GetRun().GetChange(); len(b) == 16 {
		if links, err := ascagentui.ChangeLinks(ctx, ctx.Repo.Repository, env.link, [][]byte{b}, canPulls); err == nil && len(links) == 1 {
			row.ChangeURL = links[0].URL
		}
	}
	agents := ascv1.NewAgentsClient(conn)
	ascWorkflowHidePrivate(ctx, callCtx, env, agents, &row)
	if env.features {
		logged := 0
		for i := range row.Steps {
			s := &row.Steps[i]
			if s.AgentRunID == 0 || logged >= ascWorkflowsMaxLogRuns {
				continue
			}
			logged++
			ascWorkflowStepLog(ctx, callCtx, env, agents, s, canPulls)
		}
	}
	ctx.Data["AscWorkflowRun"] = row
	ctx.Data["AscWorkflowRunActive"] = row.State.Active
	ctx.Data["AscWorkflowCanAct"] = env.features && ctx.Repo.CanWrite(unit.TypeCode)
	ctx.Data["AscWorkflowPollURL"] = row.Link + "/state"
	ctx.Data["AscWorkflowCancelable"] = row.State.Active
	ctx.HTML(http.StatusOK, tplAscWorkflowsRun)
}

// ascWorkflowStepLog fills the log of one agent step. The agent run must be this repository's run (GetRun checks
// the id against the repository named in the request, and the answer's repository is compared again).
func ascWorkflowStepLog(ctx *context.Context, callCtx gocontext.Context, env *ascAgentsEnv, api ascv1.AgentsClient, s *ascWorkflowStepRun, canPulls bool) {
	if s.Private {
		return
	}
	got, err := api.GetRun(callCtx, &ascv1.GetAgentRunRequest{Repo: env.ref(), Id: s.AgentRunID})
	if err != nil {
		return
	}
	if _, ok := ascAgentRunOf(got.GetRun(), env.name, env.runsLink(), s.AgentRunID); !ok {
		return
	}
	batch, err := ascAgentsCollect(callCtx, api, env.ref(), s.AgentRunID, 0, ascAgentsBacklogWait, ascAgentsBacklogDrain, ascWorkflowsLogEvents, 128<<10)
	if err != nil {
		return
	}
	events := batch.events
	if len(events) > ascWorkflowsLogShown {
		s.LogHidden = len(events) - ascWorkflowsLogShown
		events = events[len(events)-ascWorkflowsLogShown:]
	}
	s.Log = ascAgentEventRows(events, ascAgentEventChangeURLs(ctx, ctx.Repo.Repository, env.link, events, canPulls))
}

// ascWorkflowStateJSON is the answer of the poll of a run page.
type ascWorkflowStateJSON struct {
	State  string                `json:"state"`
	Label  string                `json:"label"`
	Tone   string                `json:"tone"`
	Active bool                  `json:"active"`
	Error  string                `json:"error,omitempty"`
	Steps  []ascWorkflowStepJSON `json:"steps"`
	Meters []ascagentui.Meter    `json:"meters"`
}

type ascWorkflowStepJSON struct {
	ID    string `json:"id"`
	State string `json:"state"`
	Label string `json:"label"`
	Tone  string `json:"tone"`
	Error string `json:"error,omitempty"`
}

// AscWorkflowRunState serves `GET /{owner}/{repo}/asc/workflows/runs/{id}/state`.
func AscWorkflowRunState(ctx *context.Context) {
	env, ok := ascAgentsGate(ctx, ascAgentsOpts{level: ascAgentsRead, noFeatures: true})
	if !ok {
		return
	}
	id, ok := ascAgentsParseID(ctx.Params("id"))
	if !ok {
		ctx.NotFound("asc workflow run", nil)
		return
	}
	ascAgentsNoStore(ctx)
	conn, err := ascRPCConn()
	if err != nil {
		log.Error("asc workflows: no connection to asc serve: %v", err)
		ctx.JSON(http.StatusBadGateway, map[string]string{"error": ascagentui.Classify(err).Message})
		return
	}
	callCtx, cancel := ascAgentsReadCtx(ctx)
	defer cancel()
	resp, err := ascv1.NewWorkflowsClient(conn).GetRun(callCtx, &ascv1.GetWorkflowRunRequest{Repo: env.ref(), Id: id})
	if err != nil {
		ascAgentsFeedFail(ctx, "GetRun", err)
		return
	}
	row, ok := ascWorkflowRunOf(resp.GetRun(), env.name, env.workflowsLink(), env.link, id)
	if !ok {
		ctx.JSON(http.StatusBadGateway, map[string]string{"error": ascagentui.Classify(errAscAgentsBadAnswer).Message})
		return
	}
	ascWorkflowHidePrivate(ctx, callCtx, env, ascv1.NewAgentsClient(conn), &row)
	out := ascWorkflowStateJSON{State: row.State.Slug, Label: row.State.Label, Tone: row.State.Tone, Active: row.State.Active, Error: row.Error, Meters: row.Meters}
	for _, s := range row.Steps {
		out.Steps = append(out.Steps, ascWorkflowStepJSON{ID: s.ID, State: s.State.Slug, Label: s.State.Label, Tone: s.State.Tone, Error: s.Error})
	}
	ctx.JSON(http.StatusOK, out)
}

// AscWorkflowRunCancel serves `POST /{owner}/{repo}/asc/workflows/runs/{id}/cancel`.
func AscWorkflowRunCancel(ctx *context.Context) {
	env, ok := ascAgentsGate(ctx, ascAgentsOpts{level: ascAgentsWrite})
	if !ok {
		return
	}
	id, ok := ascAgentsParseID(ctx.Params("id"))
	if !ok {
		ctx.NotFound("asc workflow run", nil)
		return
	}
	ascAgentsLimitBody(ctx)
	back := env.workflowsLink() + "/runs/" + strconv.FormatUint(id, 10)
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
	resp, err := ascv1.NewWorkflowsClient(conn).CancelRun(callCtx, &ascv1.CancelWorkflowRunRequest{Repo: env.ref(), Id: id, Reason: reason})
	if err != nil {
		ascAgentsFlashFail(ctx, back, "CancelRun", err)
		return
	}
	if _, ok := ascWorkflowRunOf(resp.GetRun(), env.name, env.workflowsLink(), env.link, id); !ok {
		ascAgentsFlashFail(ctx, back, "CancelRun", errAscAgentsBadAnswer)
		return
	}
	ctx.Flash.Success("The workflow run was cancelled.")
	ctx.Redirect(back)
}

// AscWorkflowRunDecide serves `POST /{owner}/{repo}/asc/workflows/runs/{id}/decide` with step, decision
// (approve or reject) and an optional comment. Only a person the step names can decide; asc checks that against
// the signed-in person, and an agent never decides.
func AscWorkflowRunDecide(ctx *context.Context) {
	env, ok := ascAgentsGate(ctx, ascAgentsOpts{level: ascAgentsWrite})
	if !ok {
		return
	}
	id, ok := ascAgentsParseID(ctx.Params("id"))
	if !ok {
		ctx.NotFound("asc workflow run", nil)
		return
	}
	ascAgentsLimitBody(ctx)
	back := env.workflowsLink() + "/runs/" + strconv.FormatUint(id, 10)
	step := ctx.FormString("step")
	var approve bool
	switch ctx.FormString("decision") {
	case "approve":
		approve = true
	case "reject":
	default:
		ctx.Error(http.StatusBadRequest)
		return
	}
	if !ascagentui.ValidStepID(step) {
		ctx.Error(http.StatusBadRequest)
		return
	}
	comment, ok := ascagentui.CheckLine(ctx.FormString("comment"), ascagentui.MaxCommentBytes, true)
	if !ok {
		ctx.Flash.Error("The comment is at most 1,024 characters on one line.")
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
	resp, err := ascv1.NewWorkflowsClient(conn).DecideApproval(callCtx, &ascv1.DecideWorkflowApprovalRequest{Repo: env.ref(), Id: id, Step: step, Approve: approve, Comment: comment})
	if err != nil {
		ascAgentsFlashFail(ctx, back, "DecideApproval", err)
		return
	}
	if _, ok := ascWorkflowRunOf(resp.GetRun(), env.name, env.workflowsLink(), env.link, id); !ok {
		ascAgentsFlashFail(ctx, back, "DecideApproval", errAscAgentsBadAnswer)
		return
	}
	if approve {
		ctx.Flash.Success("Approved.")
	} else {
		ctx.Flash.Success("Rejected.")
	}
	ctx.Redirect(back)
}

// AscWorkflowRunRerun serves `POST /{owner}/{repo}/asc/workflows/runs/{id}/rerun`: a new run of the same workflow
// with the same inputs and change. The definition that runs is the one on the trunk tip now, as for any trigger.
func AscWorkflowRunRerun(ctx *context.Context) {
	env, ok := ascAgentsGate(ctx, ascAgentsOpts{level: ascAgentsWrite, notArchived: true})
	if !ok {
		return
	}
	id, ok := ascAgentsParseID(ctx.Params("id"))
	if !ok {
		ctx.NotFound("asc workflow run", nil)
		return
	}
	ascAgentsLimitBody(ctx)
	back := env.workflowsLink() + "/runs/" + strconv.FormatUint(id, 10)
	conn, err := ascRPCConn()
	if err != nil {
		ascAgentsFlashFail(ctx, back, "connection", err)
		return
	}
	api := ascv1.NewWorkflowsClient(conn)
	readCtx, cancelRead := ascAgentsReadCtx(ctx)
	got, err := api.GetRun(readCtx, &ascv1.GetWorkflowRunRequest{Repo: env.ref(), Id: id})
	cancelRead()
	if err != nil {
		ascAgentsFlashFail(ctx, back, "GetRun", err)
		return
	}
	old := got.GetRun()
	if _, ok := ascWorkflowRunOf(old, env.name, env.workflowsLink(), env.link, id); !ok {
		ascAgentsFlashFail(ctx, back, "GetRun", errAscAgentsBadAnswer)
		return
	}
	req := &ascv1.TriggerWorkflowRequest{Repo: env.ref(), Name: old.GetWorkflow(), Inputs: map[string]string{}}
	for k, v := range old.GetInputs() {
		if ascagentui.ValidInputName(k) && len(v) <= ascagentui.MaxInputValue && len(req.Inputs) < ascWorkflowsMaxInputs {
			req.Inputs[k] = v
		}
	}
	if len(old.GetChange()) == 16 {
		req.Change = old.GetChange()
	}
	callCtx, done, ok := ascAgentsWriteCtx(ctx, ctx.FormTrim("op"))
	if !ok {
		return
	}
	defer done()
	resp, err := api.Trigger(callCtx, req)
	if err != nil {
		ascAgentsFlashFail(ctx, back, "Trigger", err)
		return
	}
	row, ok := ascWorkflowRunOf(resp.GetRun(), env.name, env.workflowsLink(), env.link, 0)
	if !ok || row.Workflow != old.GetWorkflow() {
		ascAgentsFlashFail(ctx, back, "Trigger", errAscAgentsBadAnswer)
		return
	}
	ctx.Flash.Success("Started again as run " + strconv.FormatUint(row.ID, 10) + ".")
	ctx.Redirect(row.Link)
}

// ascWorkflowSecretsCard marks which of the secrets the workflow reads are set in the repository. Only an administrator of the
// repository may list its secrets (asc checks again); for anyone else the card still names what the workflow reads and says nothing
// about whether it is set. A value is never asked for.
func ascWorkflowSecretsCard(ctx *context.Context, conn grpc.ClientConnInterface, env *ascAgentsEnv, def *ascWorkflowDef) {
	if ctx.Doer == nil || len(def.SecretsUsed) == 0 {
		return
	}
	uctx, release, err := asctrust.UserCall(ctx.Req.Context(), ctx.Doer, asctrust.CallRepoAdmin, ctx.Repo.Repository)
	if err != nil {
		return
	}
	defer release()
	cctx, cancel := gocontext.WithTimeout(uctx, ascAgentsCallTimeout)
	defer cancel()
	resp, err := ascv1.NewWorkflowsClient(conn).ListSecrets(cctx, &ascv1.ListWorkflowSecretsRequest{Repo: env.ref()})
	if err != nil {
		log.Warn("asc workflows: ListSecrets failed: %v", err)
		return
	}
	have := map[string]bool{}
	for i, sec := range resp.GetSecrets() {
		if i >= 500 {
			break
		}
		have[ascagentui.CleanLine(sec.GetName(), 64)] = true
	}
	for i := range def.SecretsUsed {
		def.SecretsUsed[i].Checked = true
		def.SecretsUsed[i].Set = have[def.SecretsUsed[i].Name]
	}
	for i := range def.Steps {
		for j := range def.Steps[i].Secrets {
			use := &def.Steps[i].Secrets[j]
			use.Checked = true
			use.Set = have[use.Name]
		}
	}
}
