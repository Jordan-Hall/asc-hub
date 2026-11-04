// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	gocontext "context"
	"errors"
	"strings"
	"unicode/utf8"

	issues_model "forgejo.org/models/issues"
	pull_model "forgejo.org/models/pull"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unit"
	"forgejo.org/modules/ascclient"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/gitrepo"
	"forgejo.org/modules/log"
	"forgejo.org/modules/setting"
	"forgejo.org/services/ascagentui"
	"forgejo.org/services/ascbabysit"
	"forgejo.org/services/automerge"
	"forgejo.org/services/context"
	pull_service "forgejo.org/services/pull"
)

// Options of the open-change form (board NewChange): open it as a draft (the first draft prefix of the instance goes in
// front of the title), the stack it sits in, land it when it is approved and green (the merge queue for a change of a
// stack, the scheduled merge of the hub for one on its own) and ask an agent to watch its checks (services/ascbabysit).
// The change opens whatever an option does: an option that cannot be done is named in a warning.

const (
	ascOptDraftField   = "asc_draft"
	ascOptLandField    = "asc_land"
	ascOptBabysitField = "asc_babysit"
	ascOptAgentField   = "asc_babysit_agent"
	// ascOptAgentsMax is how many agents the form offers.
	ascOptAgentsMax = 10
)

// AscNewChangeOptions is what the Options card of the open-change form offers.
type AscNewChangeOptions struct {
	DraftPrefix string   // "" when the instance has no draft prefix
	Below       int      // changes of the stack below this one
	CanLand     bool     // the person may merge here
	Agents      []string // agents that may write here, the busiest first; nil when no agent can be asked
	Runs        int      // the most runs an agent makes for the change
}

// Any reports whether the card has a row.
func (o *AscNewChangeOptions) Any() bool {
	return o.DraftPrefix != "" || o.Below > 0 || o.CanLand || len(o.Agents) > 0
}

// Below is how many changes of the stack sit under the change the form opens.
func (r *AscNewChangeRail) Below() int {
	if r == nil {
		return 0
	}
	for i, c := range r.Stack {
		if c.Current {
			return len(r.Stack) - 1 - i + r.StackMore
		}
	}
	return 0
}

// ascOptAgentsOf keeps the agents that may write to the repository; with none, it offers the default agent.
func ascOptAgentsOf(roster []ascagentui.Roster) []string {
	var out []string
	for _, a := range roster {
		if a.Scope != "read-only" && ascagentui.ValidAgentName(a.Name) && len(out) < ascOptAgentsMax {
			out = append(out, a.Name)
		}
	}
	if len(out) == 0 {
		out = []string{ascAgentsDefaultAgent}
	}
	return out
}

// prepareAscNewChangeOptions puts the Options card into ctx.Data["AscNewChangeOptions"]. It runs after the rail.
func prepareAscNewChangeOptions(ctx *context.Context) {
	repo := ctx.Repo.Repository
	if !setting.Asc.Enabled || !ctx.IsSigned || repo.GetStorageBackend() != string(gitrepo.StorageBackendAsc) {
		return
	}
	opts := &AscNewChangeOptions{CanLand: ctx.Repo.CanWrite(unit.TypeCode), Runs: ascbabysit.MaxRuns}
	if p := setting.Repository.PullRequest.WorkInProgressPrefixes; len(p) > 0 {
		opts.DraftPrefix = p[0]
	}
	rail, _ := ctx.Data["AscNewChangeRail"].(*AscNewChangeRail)
	opts.Below = rail.Below()
	// An agent pushes to the branch of the change, which must be in this repository.
	if head, _ := ctx.Data["HeadRepo"].(*repo_model.Repository); head != nil && head.ID == repo.ID {
		opts.Agents = ascNewChangeAgents(ctx)
	}
	if opts.Any() {
		ctx.Data["AscNewChangeOptions"] = opts
	}
}

// ascNewChangeAgents asks asc for the agents of the repository; nil when asc does not answer.
func ascNewChangeAgents(ctx *context.Context) []string {
	name, err := ascGraphRepoName(ctx.Repo.Repository)
	if err != nil {
		return nil
	}
	conn, err := ascRPCConn()
	if err != nil {
		return nil
	}
	callCtx, cancel := gocontext.WithTimeout(ascclient.WithSystem(ctx), ascBriefTimeout)
	defer cancel()
	resp, err := ascv1.NewAgentsClient(conn).ListAgents(callCtx, &ascv1.ListRepoAgentsRequest{Repo: &ascv1.RepoRef{Name: name}})
	if err != nil {
		log.Warn("asc new change options: agents: %v", err)
		return nil
	}
	return ascOptAgentsOf(ascagentui.RosterOf(resp.GetAgents()))
}

// ascNewChangeTitle is the title of the change: with the draft prefix in front when the form asks for a draft. ok is
// false when the prefix does not fit; the form has answered then.
func ascNewChangeTitle(ctx *context.Context, title string) (string, bool) {
	p := setting.Repository.PullRequest.WorkInProgressPrefixes
	if ctx.Req.PostForm.Get(ascOptDraftField) == "" || len(p) == 0 || issues_model.HasWorkInProgressPrefix(title) {
		return title, true
	}
	out := p[0] + " " + title
	if utf8.RuneCountInString(out) > 255 {
		ctx.JSONError(ctx.Tr("asc_rp.opt_draft_long", p[0]))
		return "", false
	}
	return out, true
}

// ascApplyNewChangeOptions does what the options of the form ask for the change that was just opened.
func ascApplyNewChangeOptions(ctx *context.Context, issue *issues_model.Issue, pr *issues_model.PullRequest) {
	form := ctx.Req.PostForm
	if form.Get(ascOptLandField) != "" {
		ascLandWhenGreen(ctx, issue, pr)
	}
	if form.Get(ascOptBabysitField) != "" {
		agent := strings.TrimSpace(form.Get(ascOptAgentField))
		if err := ascbabysit.Watch(ctx, ctx.Doer, pr, agent); err != nil {
			log.Info("asc new change: babysit of #%d by %q: %v", issue.Index, agent, err)
			ctx.Flash.Warning(ctx.Tr("asc_rp.opt_babysit_not"))
		}
	}
}

// ascLandWhenGreen queues a change of a stack, or schedules the merge of the hub for a change on its own.
func ascLandWhenGreen(ctx *context.Context, issue *issues_model.Issue, pr *issues_model.PullRequest) {
	pr.Issue = issue
	if issues_model.HasWorkInProgressPrefix(issue.Title) {
		ctx.Flash.Warning(ctx.Tr("asc_rp.opt_land_draft"))
		return
	}
	_, err := automerge.AscQueueEnqueue(ctx, ctx.Doer, pr)
	if err == nil {
		return
	}
	if !errors.Is(err, pull_service.ErrAscStackNotStacked) {
		log.Info("asc new change: queue of #%d: %v", issue.Index, err)
		ctx.Flash.Warning(ctx.Tr("asc_rp.opt_land_not"))
		return
	}
	if ok, err := pull_service.IsUserAllowedToMerge(ctx, pr, ctx.Repo.Permission, ctx.Doer); err != nil || !ok {
		ctx.Flash.Warning(ctx.Tr("asc_rp.opt_land_not"))
		return
	}
	cfg := ctx.Repo.Repository.MustGetUnit(ctx, unit.TypePullRequests).PullRequestsConfig()
	style := cfg.GetDefaultMergeStyle()
	message, _, err := pull_service.GetDefaultMergeMessage(ctx, ctx.Repo.GitRepo, pr, style)
	if err == nil {
		_ = pull_model.DeleteScheduledAutoMerge(ctx, pr.ID)
		_, err = automerge.ScheduleAutoMerge(ctx, ctx.Doer, pr, style, message, cfg.DefaultDeleteBranchAfterMerge)
	}
	if err != nil {
		log.Warn("asc new change: scheduled merge of #%d: %v", issue.Index, err)
		ctx.Flash.Warning(ctx.Tr("asc_rp.opt_land_not"))
	}
}
