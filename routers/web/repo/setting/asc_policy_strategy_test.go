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
