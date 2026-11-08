// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package setting

import (
	"testing"

	"forgejo.org/services/asctrust"

	"github.com/stretchr/testify/assert"
)

// Board RepoSettingsPolicy, "How changes land": each strategy is named by what it does to a stack, and "squash"
// lands the stack as one change.
func TestAscPolicyStrategyChoices(t *testing.T) {
	p := &ascPolicyPage{
		View:      &asctrust.PolicyView{Present: true, Usable: true, Maintainer: true},
		PolicyURL: "/user2/repo1/settings/asc/policy", ToggleOps: map[string]string{},
		Form:       asctrust.PolicyForm{MergeStrategy: "squash"},
		Strategies: asctrust.MergeStrategies,
	}
	html := renderSettingsPage(t, t.TempDir(), "policy-strategy", tplAscPolicy, map[string]any{"AscPolicy": p})

	assert.Contains(t, html, `<option value="rebase">Rebase, keep every change</option>`)
	assert.Contains(t, html, `<option value="rebase-disjoint">Rebase when changes do not overlap, keep every change</option>`)
	assert.Contains(t, html, `<option value="fast-forward">Fast-forward only, keep every change</option>`)
	assert.Contains(t, html, `<option value="squash" selected>Squash into one change</option>`)
}

// Board RepoSettingsRules, "How stacks land": the Rules card says whether a stack keeps every change or lands as one.
func TestAscRulesHowStacksLand(t *testing.T) {
	page := func(squash bool) string {
		rules := &ascRulesPage{Protection: &asctrust.BranchRule{Branch: "main"}, Squash: squash, PolicyURL: "/user2/repo1/settings/asc/policy"}
		return renderSettingsPage(t, t.TempDir(), "rules-lands", tplBranches, map[string]any{"AscRules": rules})
	}
	assert.Contains(t, page(true), `<div data-testid="asc-rules-lands"><dt>How stacks land</dt><dd>Squash into one change</dd></div>`)
	assert.Contains(t, page(false), `<div data-testid="asc-rules-lands"><dt>How stacks land</dt><dd>Keep every change</dd></div>`)
}

// Board RepoSettingsRules, "Longest stack" and "Clean-up after landing": the card shows the `[stacks]` keys, and
// what applies when the policy leaves them out.
func TestAscRulesStackLimits(t *testing.T) {
	page := func(depth, days int64) string {
		rules := &ascRulesPage{Protection: &asctrust.BranchRule{Branch: "main"}, MaxDepth: depth, CleanupDays: days, PolicyURL: "/user2/repo1/settings/asc/policy"}
		return renderSettingsPage(t, t.TempDir(), "rules-stacks", tplBranches, map[string]any{"AscRules": rules})
	}
	html := page(8, 7)
	assert.Contains(t, html, `<div data-testid="asc-rules-depth"><dt>Longest stack</dt><dd>8 changes</dd></div>`)
	assert.Contains(t, html, `<div data-testid="asc-rules-cleanup"><dt>Clean-up after landing</dt><dd>7 days</dd></div>`)
	html = page(1, 1)
	assert.Contains(t, html, `<dd>1 change</dd>`)
	assert.Contains(t, html, `<dd>1 day</dd>`)
	html = page(0, 0)
	assert.Contains(t, html, `<div data-testid="asc-rules-depth"><dt>Longest stack</dt><dd>No limit</dd></div>`)
	assert.Contains(t, html, `<div data-testid="asc-rules-cleanup"><dt>Clean-up after landing</dt><dd>The server's setting</dd></div>`)
}

// Board RepoSettingsPolicy, Stacks: the two fields show the policy's values and stay empty when it sets none.
func TestAscPolicyStackFields(t *testing.T) {
	render := func(f asctrust.PolicyForm) string {
		p := &ascPolicyPage{
			View:      &asctrust.PolicyView{Present: true, Usable: true, Maintainer: true},
			PolicyURL: "/user2/repo1/settings/asc/policy", ToggleOps: map[string]string{},
			Form: f, Strategies: asctrust.MergeStrategies,
		}
		return renderSettingsPage(t, t.TempDir(), "policy-stacks", tplAscPolicy, map[string]any{"AscPolicy": p})
	}
	html := render(asctrust.PolicyForm{MaxStackDepth: 8, CleanupDays: 7})
	assert.Contains(t, html, `name="max_depth" type="number" min="1" max="100" inputmode="numeric" value="8"`)
	assert.Contains(t, html, `name="cleanup_days" type="number" min="1" max="3650" inputmode="numeric" value="7"`)
	html = render(asctrust.PolicyForm{})
	assert.Contains(t, html, `name="max_depth" type="number" min="1" max="100" inputmode="numeric" value=""`)
	assert.Contains(t, html, `Leave empty to use the server's setting.`)
}
