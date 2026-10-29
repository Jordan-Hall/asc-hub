// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package setting

import (
	"forgejo.org/modules/log"
	"forgejo.org/modules/setting"
	"forgejo.org/services/asctrust"
	"forgejo.org/services/context"
)

// The Rules page of an asc repository shows the asc policy in force instead of the rules of Forgejo: the rules of
// trunk are `.asc/policy.toml` (services/asctrust/protection.go has the mapping). The page is for administrators,
// like the rest of the branch settings.

// ascRulesRepo reports whether the repository takes its branch rules from the asc policy.
func ascRulesRepo(ctx *context.Context) bool {
	return setting.Asc.Enabled && ctx.Repo.Repository != nil && ctx.Repo.Repository.GetStorageBackend() == "asc"
}

// ascRulesPage is the template data under .AscRules.
type ascRulesPage struct {
	Protection *asctrust.BranchRule
	PolicyURL  string
	Problem    string
}

// prepareAscRules sets AscRules for an asc repository: the protection read from the policy in force.
func prepareAscRules(ctx *context.Context) {
	if !ascRulesRepo(ctx) {
		return
	}
	repo := ctx.Repo.Repository
	page := &ascRulesPage{PolicyURL: ctx.Repo.RepoLink + "/settings/asc/policy"}
	ctx.Data["AscRules"] = page
	name, err := asctrust.RepoName(repo)
	conn, cerr := asctrust.Conn()
	if err != nil || cerr != nil || !asctrust.HasFeature(ctx, asctrust.FeaturePolicy) {
		page.Problem = ctx.Locale.TrString("repo.settings.asc_rules.unavailable")
		return
	}
	callCtx, release, err := asctrust.UserCall(ctx, ctx.Doer, asctrust.CallRepoRead, repo)
	if err != nil {
		page.Problem = ctx.Locale.TrString("repo.settings.asc_rules.unavailable")
		return
	}
	defer release()
	pp, err := asctrust.LoadPolicyPage(callCtx, conn, name)
	if err != nil {
		log.Error("asc rules of %s: %v", repo.FullName(), err)
		page.Problem = ctx.Locale.TrString("repo.settings.asc_rules.unavailable")
		return
	}
	if !pp.View.Present {
		page.Problem = ctx.Locale.TrString("repo.settings.asc_rules.nopolicy")
		return
	}
	p := asctrust.ProtectionOfText(pp.View.Text, repo.DefaultBranch)
	page.Protection = &p
}
