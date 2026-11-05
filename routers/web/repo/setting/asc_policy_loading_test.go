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
