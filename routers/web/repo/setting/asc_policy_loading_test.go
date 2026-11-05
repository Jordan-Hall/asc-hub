// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package setting

import (
	"strings"
	"testing"

	"forgejo.org/services/asctrust"

	"github.com/stretchr/testify/assert"
)

// Board RepoSettingsPolicy, "Loading state": while asc checks, reviews or saves the policy, a skeleton of the file
// header, the tabs and the fields takes the place of the policy (asc-page-loading.js).
func TestAscPolicyLoadingState(t *testing.T) {
	p := &ascPolicyPage{
		View:      &asctrust.PolicyView{Present: true, Usable: true, Maintainer: true, Protections: []asctrust.Protection{{Key: "signed", Title: "Signed changes"}}},
		PolicyURL: "/user2/repo1/settings/asc/policy", ToggleOps: map[string]string{"signed": "op-1"},
	}
	html := renderSettingsPage(t, t.TempDir(), "policy", tplAscPolicy, map[string]any{"AscPolicy": p})

	assert.Contains(t, html, `role="status" data-loading-skel data-testid="asc-policy-loading" hidden`)
	assert.Contains(t, html, "Asking asc about the policy")
	assert.Equal(t, 3, strings.Count(html, "ds-sk ast-policy-sk-tab"))
	assert.Equal(t, 4, strings.Count(html, `class="ast-policy-sk-field"`))
	skel := strings.Index(html, `data-testid="asc-policy-loading"`)
	body := strings.Index(html, "data-loading-body")
	assert.True(t, skel > 0 && body > skel, "the skeleton sits before the policy it replaces")
	rest := html[body:]
	for _, action := range []string{"/form", "/propose", "/protection"} {
		assert.Contains(t, rest, `data-loading-form action="/user2/repo1/settings/asc/policy`+action+`"`, action)
	}

	none := renderSettingsPage(t, t.TempDir(), "policy-unsupported", tplAscPolicy, map[string]any{"AscPolicy": &ascPolicyPage{Unsupported: true}})
	assert.NotContains(t, none, "data-loading-skel", "nothing loads when asc cannot serve the policy")
}

// Board RepoSettingsPolicy, Changes tab: the diff of the pending edit and a "Changing policy is a change" panel that
// names the reviewers, the maintainers of the policy.
func TestAscPolicyChangesReviewers(t *testing.T) {
	page := func(maintainers []string) string {
		p := &ascPolicyPage{
			View:      &asctrust.PolicyView{Present: true, Usable: true, Maintainer: true},
			PolicyURL: "/user2/repo1/settings/asc/policy", ToggleOps: map[string]string{},
			Dirty: true, Diff: []asctrust.DiffLine{{Op: "+", Text: "require_approvals = 2"}}, Maintainers: maintainers,
		}
		return renderSettingsPage(t, t.TempDir(), "policy-changes", tplAscPolicy, map[string]any{"AscPolicy": p})
	}
	html := page([]string{"ada", "<grace>"})
	assert.Contains(t, html, `data-testid="asc-policy-review-panel"`)
	assert.Contains(t, html, "Changing policy is a change")
	assert.Contains(t, html, `<li><strong>ada</strong></li><li><strong>&lt;grace&gt;</strong></li>`)
	assert.Contains(t, html, `<span aria-hidden="true">&#43; </span>require_approvals = 2`)

	none := page(nil)
	assert.Contains(t, none, `data-testid="asc-policy-no-reviewers"`)
	assert.NotContains(t, none, `data-testid="asc-policy-reviewers"`)
}
