// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package setting

import (
	"encoding/hex"

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
	// Squash is true when a stack lands on trunk as one change (`[merge] strategy = "squash"`); otherwise every
	// change of the stack lands.
	Squash bool
	// MaxDepth is `[stacks] max_depth`, the most changes one stack may have; 0 for no limit.
	MaxDepth int64
	// CleanupDays is `[stacks] cleanup_days`; 0 when the server's setting applies.
	CleanupDays int64
	// RestackOnMove is `[stacks] restack_on_main_move`: the hub restacks open stacks when the default branch moves.
	RestackOnMove bool
	// WritersMayPush is `[stacks] collaborators_may_push`: writers may update a stack they did not start.
	WritersMayPush bool
	// KeepApprovals is `[review] keep_approvals_on_restack`: approvals stay when only the base changed.
	KeepApprovals bool
	PolicyURL     string
	Problem       string
	// Refs are the `[refs]` rules of the policy, one row each under trunk.
	Refs []ascRefRow
	// RulesURL is the Rules page; AddURL opens the side panel for a new rule. SaveURL and RemoveURL take the panel
	// and the removal of a rule.
	RulesURL, AddURL, SaveURL, RemoveURL string
	// Base is the changeset the policy was read from (hex) and Op the operation id of a proposal from this page.
	Base, Op string
	// LandWords are the words of "Who can land", in order.
	LandWords []string
	// Panel is the open side panel, or nil.
	Panel *ascRefPanel
}

// ascRefRow is one `[refs]` rule on the Rules page.
type ascRefRow struct {
	asctrust.RefRule
	EditURL string
}

// prepareAscRules sets AscRules for an asc repository: the protection read from the policy in force.
func prepareAscRules(ctx *context.Context) {
	if !ascRulesRepo(ctx) {
		return
	}
	repo := ctx.Repo.Repository
	rules := ascRulesLink(ctx)
	page := &ascRulesPage{
		PolicyURL: ctx.Repo.RepoLink + "/settings/asc/policy",
		RulesURL:  rules, AddURL: rules + "?ref_add=1",
		SaveURL: ctx.Repo.RepoLink + "/settings/asc/refs", RemoveURL: ctx.Repo.RepoLink + "/settings/asc/refs/remove",
		Op: asctrust.NewOpID(), LandWords: asctrust.LandWords,
	}
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
	page.Squash = pp.View.Strategy == "squash"
	f := asctrust.FormOfText(pp.View.Text)
	page.MaxDepth, page.CleanupDays = f.MaxStackDepth, f.CleanupDays
	page.RestackOnMove, page.WritersMayPush, page.KeepApprovals = f.RestackOnMove, f.WritersMayPush, f.KeepApprovals
	page.Base = hex.EncodeToString(pp.Base)
	for _, r := range asctrust.RefRulesOfText(pp.View.Text) {
		page.Refs = append(page.Refs, ascRefRow{RefRule: r, EditURL: ascRefEditLink(rules, r.Pattern)})
	}
	ascRefPanelOf(ctx, page)
}
