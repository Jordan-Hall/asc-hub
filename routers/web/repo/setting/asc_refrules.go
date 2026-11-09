// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package setting

import (
	"net/url"
	"slices"
	"strings"

	repo_model "forgejo.org/models/repo"
	"forgejo.org/routers/web/shared/trustweb"
	"forgejo.org/services/asctrust"
	"forgejo.org/services/context"
)

// The protected refs of an asc repository other than trunk are the `[refs."<pattern>"]` rules of its policy. The
// Rules page lists them under trunk; Add and Edit open a side panel on the same page (`?ref_add=1`,
// `?ref_edit=<pattern>`), and saving or removing a rule proposes the edited policy as a change under review, like a
// switch of the policy page. asc enforces the rules once the change lands.

const maxRefRuleForm = 8 << 10

// ascRefPanel is the side panel under .AscRules.Panel.
type ascRefPanel struct {
	// Was is the pattern of the rule being edited; empty for a new rule.
	Was  string
	Tags bool
	// Name is the pattern without `refs/tags/`.
	Name    string
	Rule    asctrust.RefRule
	Problem string
}

func ascRulesLink(ctx *context.Context) string { return ctx.Repo.RepoLink + "/settings/branches" }

// ascRefPanelOf opens the side panel the query asks for, once the rules are read. A posted form that was refused
// is shown again as typed.
func ascRefPanelOf(ctx *context.Context, page *ascRulesPage) {
	if posted, ok := ctx.Data["AscRefPanelPosted"].(*ascRefPanel); ok {
		page.Panel = posted
		return
	}
	if ctx.FormString("ref_add") != "" {
		page.Panel = &ascRefPanel{Rule: asctrust.RefRule{Land: []string{asctrust.LandQueue, asctrust.LandMaintainers}}}
		return
	}
	edit := ctx.FormString("ref_edit")
	if edit == "" {
		return
	}
	i := slices.IndexFunc(page.Refs, func(r ascRefRow) bool { return r.Pattern == edit })
	if i < 0 {
		return
	}
	r := page.Refs[i].RefRule
	page.Panel = &ascRefPanel{Was: r.Pattern, Tags: r.IsTags(), Name: r.Name(), Rule: r}
}

// refRuleOfForm reads the side panel: kind, name, the ticked land words and the three switches.
func refRuleOfForm(ctx *context.Context) *ascRefPanel {
	p := &ascRefPanel{
		Was:  strings.TrimSpace(ctx.Req.PostFormValue("was")),
		Tags: ctx.Req.PostFormValue("kind") == "tags",
		Name: strings.TrimSpace(ctx.Req.PostFormValue("name")),
	}
	pattern := strings.TrimPrefix(strings.TrimPrefix(p.Name, "refs/heads/"), "/")
	if p.Tags {
		pattern = "refs/tags/" + strings.TrimPrefix(p.Name, "refs/tags/")
	}
	p.Rule = asctrust.RefRule{
		Pattern: pattern,
		Land:    ctx.Req.PostForm["land"],
		Signed:  ctx.Req.PostFormValue("signed") == "on",
		Force:   ctx.Req.PostFormValue("force") == "on",
		Delete:  ctx.Req.PostFormValue("delete") == "on",
	}
	return p
}

// ascRefRefused draws the Rules page again with the panel as posted and the problem in it.
func ascRefRefused(ctx *context.Context, p *ascRefPanel, problem string) {
	p.Problem = problem
	ctx.Data["AscRefPanelPosted"] = p
	ProtectedBranchRules(ctx)
}

// AscRefRuleSave serves POST .../settings/asc/refs with was, kind, name, land, signed, force, delete, base and op:
// the rule is set in the policy in force and the result is proposed.
func AscRefRuleSave(ctx *context.Context) {
	repo, name, ok := trustweb.AdminRepo(ctx)
	if !ok {
		return
	}
	if !trustweb.ParseForm(ctx, maxRefRuleForm) {
		return
	}
	p := refRuleOfForm(ctx)
	text, _, problem := ascCurrentPolicy(ctx, repo, name)
	if problem != "" {
		ascRefRefused(ctx, p, problem)
		return
	}
	next, err := asctrust.ApplyRefRule(text, p.Was, p.Rule)
	if err != nil {
		ascRefRefused(ctx, p, trustweb.FailureOf("ref rule", err).Localized(ctx))
		return
	}
	message := "policy: protect " + p.Rule.Pattern
	if p.Was != "" {
		message = "policy: change the rule of " + p.Rule.Pattern
		if p.Was != p.Rule.Pattern {
			message = "policy: move the rule of " + p.Was + " to " + p.Rule.Pattern
		}
	}
	ascRefPropose(ctx, repo, name, p, next, message)
}

// AscRefRuleRemove serves POST .../settings/asc/refs/remove with pattern, base and op.
func AscRefRuleRemove(ctx *context.Context) {
	repo, name, ok := trustweb.AdminRepo(ctx)
	if !ok {
		return
	}
	if !trustweb.ParseForm(ctx, maxRefRuleForm) {
		return
	}
	pattern := ctx.Req.PostFormValue("pattern")
	text, _, problem := ascCurrentPolicy(ctx, repo, name)
	var next string
	if problem == "" {
		var err error
		next, err = asctrust.RemoveRefRule(text, pattern)
		if err != nil {
			problem = trustweb.FailureOf("ref rule", err).Localized(ctx)
		}
	}
	if problem != "" {
		ctx.Flash.Error(problem)
		ctx.Redirect(ascRulesLink(ctx))
		return
	}
	ascRefPropose(ctx, repo, name, nil, next, "policy: stop protecting "+pattern)
}

// ascRefPropose proposes next against the base the page was drawn from and sends the person to the pull request,
// or back to the Rules page with what happened.
func ascRefPropose(ctx *context.Context, repo *repo_model.Repository, name string, p *ascRefPanel, next, message string) {
	redirect, proposed, problem := ascPolicyPropose(ctx, repo, name, next, message, ctx.Req.PostFormValue("op"), ctx.Req.PostFormValue("base"))
	switch {
	case redirect != "":
		ctx.Redirect(redirect)
	case proposed != nil:
		ctx.Flash.Success(ctx.Locale.TrString("asc_rp.p_proposed_pr", proposed.Change))
		ctx.Redirect(ascRulesLink(ctx))
	case p != nil:
		ascRefRefused(ctx, p, problem)
	default:
		ctx.Flash.Error(problem)
		ctx.Redirect(ascRulesLink(ctx))
	}
}

// ascRefEditLink is the link that opens the panel for the rule of pattern.
func ascRefEditLink(base, pattern string) string {
	return base + "?ref_edit=" + url.QueryEscape(pattern)
}
