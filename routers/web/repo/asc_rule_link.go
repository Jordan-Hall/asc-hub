// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	"html"
	"html/template"
	"net/url"
	"strconv"

	git_model "forgejo.org/models/git"
	"forgejo.org/modules/log"
	"forgejo.org/services/context"
)

// The refusal to delete a protected branch or tag names the rule that protects it: "Open rule" goes to the rule's
// settings. Only a person who may change the rule (a repository admin) gets the link; anyone else gets the sentence.

// ascWithRuleLink adds "Open rule" to msg when link is not "".
func ascWithRuleLink(ctx *context.Context, msg template.HTML, link string) template.HTML {
	if link == "" {
		return msg
	}
	return msg + template.HTML(` <a href="`+html.EscapeString(link)+`" data-testid="open-rule">`+
		html.EscapeString(ctx.Locale.TrString("asc_rp.open_rule"))+`</a>`)
}

// protectedBranchRuleLink is the settings page of the rule that protects branch, "" when the person may not change it
// or no rule matches.
func protectedBranchRuleLink(ctx *context.Context, branch string) string {
	if !ctx.Repo.IsAdmin() {
		return ""
	}
	rule, err := git_model.GetFirstMatchProtectedBranchRule(ctx, ctx.Repo.Repository.ID, branch)
	if err != nil {
		log.Error("protected branch rule of %q: %v", branch, err)
		return ""
	}
	if rule == nil {
		return ""
	}
	return ctx.Repo.RepoLink + "/settings/branches/edit?" + url.Values{"rule_name": {rule.RuleName}}.Encode()
}

// protectedTagRuleLink is the settings page of the rule that protects tag, "" when the person may not change it or no
// rule matches.
func protectedTagRuleLink(ctx *context.Context, tag string) string {
	if !ctx.Repo.IsAdmin() {
		return ""
	}
	rules, err := git_model.GetProtectedTags(ctx, ctx.Repo.Repository.ID)
	if err != nil {
		log.Error("protected tag rules: %v", err)
		return ""
	}
	rule := git_model.FirstProtectedTagFor(rules, tag)
	if rule == nil {
		return ""
	}
	return ctx.Repo.RepoLink + "/settings/tags/" + strconv.FormatInt(rule.ID, 10)
}
