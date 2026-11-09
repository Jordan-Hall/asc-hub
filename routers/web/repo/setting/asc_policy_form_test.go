// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package setting

import (
	"strings"
	"testing"

	"forgejo.org/services/asctrust"

	"github.com/stretchr/testify/assert"
)

// Board RepoSettingsPolicy, Form view: the approvals stepper, the checks and paths as chips with Add, switches, the
// longest run in minutes and the locked "A person approves every agent change".
func TestAscPolicyFormControls(t *testing.T) {
	page := func(f asctrust.PolicyForm) string {
		p := &ascPolicyPage{
			View:      &asctrust.PolicyView{Present: true, Usable: true, Maintainer: true},
			PolicyURL: "/user2/repo1/settings/asc/policy", ToggleOps: map[string]string{}, Form: f,
		}
		return renderSettingsPage(t, t.TempDir(), "policy-form", tplAscPolicy, map[string]any{"AscPolicy": p})
	}
	html := page(asctrust.PolicyForm{
		RequireApprovals: 2, RequireChecks: []string{"build", "<lint>"}, AuthorMayApprove: true, KeepApprovals: true,
		AgentScope: []string{"src/**"}, AgentDenied: []string{"infra/**"}, MaxRuntimeSecs: 90, HumanApproval: true,
	})

	// the stepper: two buttons the script shows around the number field
	assert.Contains(t, html, `data-policy-stepper`)
	assert.Equal(t, 2, strings.Count(html, `aria-controls="pf-approvals" hidden`))
	assert.Contains(t, html, `aria-label="One approval fewer"`)
	assert.Contains(t, html, `name="approvals" type="number" min="0" max="64" inputmode="numeric" value="2"`)

	// the chips: one ticked checkbox for each entry, escaped, and an Add field per list
	assert.Contains(t, html, `<input type="checkbox" name="checks" value="build" checked>`)
	assert.Contains(t, html, `<input type="checkbox" name="checks" value="&lt;lint&gt;" checked><span class="ds-mono">&lt;lint&gt;</span>`)
	assert.Contains(t, html, `<input type="checkbox" name="agent_scope" value="src/**" checked>`)
	assert.Contains(t, html, `<input type="checkbox" name="agent_denied" value="infra/**" checked>`)
	for _, name := range []string{"checks_add", "agent_scope_add", "agent_denied_add"} {
		assert.Contains(t, html, `name="`+name+`"`, name)
	}
	assert.Equal(t, 3, strings.Count(html, `data-chips-add hidden>Add</button>`))
	assert.Contains(t, html, `aria-label="Add a check"`)
	assert.Contains(t, html, "Untick an entry to remove it.")
	assert.NotContains(t, html, `<textarea id="pf-checks"`, "the lists are no longer text areas")

	// switches, not plain checkboxes
	assert.Contains(t, html, `<input type="checkbox" role="switch" name="author_may_approve" checked>`)
	assert.Contains(t, html, `<input type="checkbox" role="switch" name="keep_approvals" checked>`)
	assert.Contains(t, html, `<input type="checkbox" role="switch" name="require_signed">`)
	assert.NotContains(t, html, `class="ds-check"><input type="checkbox" name="queue"`)

	// the longest run in minutes, rounded up, with the seconds kept for an unchanged save
	assert.Contains(t, html, "Longest run, in minutes")
	assert.Contains(t, html, `name="max_minutes" type="number" min="0" inputmode="numeric" value="2"`)
	assert.Contains(t, html, `<input type="hidden" name="max_runtime" value="90">`)

	// the locked human approval switch
	assert.Contains(t, html, `data-testid="asc-policy-human"><input type="checkbox" role="switch" disabled aria-describedby="pf-human-help" checked>`)
	assert.Contains(t, html, "the policy never lets an agent approve its own work")

	off := page(asctrust.PolicyForm{})
	assert.Contains(t, off, `role="switch" disabled aria-describedby="pf-human-help"><span`, "off in the TOML: the locked switch is off")
	assert.Contains(t, off, "Off in the TOML view: an agent's approval counts.")
	assert.NotContains(t, off, `name="checks" value=`, "no entries, no chips")
}
