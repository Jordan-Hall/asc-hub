// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package setting

import (
	"net/http"
	"net/url"
	"strings"

	git_model "forgejo.org/models/git"
	issues_model "forgejo.org/models/issues"
	"forgejo.org/services/context"
)

// ascRuleCheck is what the rule form shows under the pattern (board RepoSettingsRules): the rule it would overlap
// and the open pull requests that target a branch it matches.
type ascRuleCheck struct {
	Overlap string `json:"overlap"`
	Open    int64  `json:"open"`
}

func ascCheckRule(ctx *context.Context, ruleID int64, pattern string) (ascRuleCheck, error) {
	var out ascRuleCheck
	if pattern == "" {
		return out, nil
	}
	other, err := git_model.AscOverlappingRule(ctx, ctx.Repo.Repository.ID, ruleID, pattern)
	if err != nil {
		return out, err
	}
	if other != nil {
		out.Overlap = other.RuleName
	}
	byBase, err := issues_model.AscOpenPullsByBase(ctx, ctx.Repo.Repository.ID)
	if err != nil {
		return out, err
	}
	out.Open = git_model.AscRuleOpenCount(pattern, byBase)
	return out, nil
}

// AscRuleCheck answers GET settings/branches/check?rule_id=&rule_name= for the rule form as you type.
func AscRuleCheck(ctx *context.Context) {
	check, err := ascCheckRule(ctx, ctx.FormInt64("rule_id"), strings.TrimSpace(ctx.FormString("rule_name")))
	if err != nil {
		ctx.ServerError("ascCheckRule", err)
		return
	}
	ctx.JSON(http.StatusOK, check)
}

// prepareAscRuleCheck fills AscRuleCheck for the rule form of an existing rule.
func prepareAscRuleCheck(ctx *context.Context, rule *git_model.ProtectedBranch) {
	check, err := ascCheckRule(ctx, rule.ID, rule.RuleName)
	if err != nil {
		ctx.ServerError("ascCheckRule", err)
		return
	}
	ctx.Data["AscRuleCheck"] = check
}

// ascRuleOverlapRefused refuses a new pattern, or a renamed one, that would protect a branch another rule protects:
// a branch answers to one rule only. It flashes the error, sends the form back and reports true.
func ascRuleOverlapRefused(ctx *context.Context, rule *git_model.ProtectedBranch, pattern string) bool {
	var ruleID int64
	back := ctx.Repo.RepoLink + "/settings/branches/edit"
	if rule != nil {
		if rule.RuleName == pattern {
			return false
		}
		ruleID = rule.ID
		back += "?rule_name=" + url.QueryEscape(rule.RuleName)
	}
	other, err := git_model.AscOverlappingRule(ctx, ctx.Repo.Repository.ID, ruleID, pattern)
	if err != nil {
		ctx.ServerError("AscOverlappingRule", err)
		return true
	}
	if other == nil {
		return false
	}
	ctx.Flash.Error(ctx.Tr("repo.settings.asc_rules.overlap", pattern, other.RuleName))
	ctx.Redirect(back)
	return true
}
