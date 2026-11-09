// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package setting

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"forgejo.org/models/perm"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/test"
	"forgejo.org/services/asctrust"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func refRulesPage() *ascRulesPage {
	rules := "/user2/repo1/settings/branches"
	return &ascRulesPage{
		Protection: &asctrust.BranchRule{Branch: "main", QueueOnly: true},
		PolicyURL:  "/user2/repo1/settings/asc/policy",
		RulesURL:   rules, AddURL: rules + "?ref_add=1",
		SaveURL: "/user2/repo1/settings/asc/refs", RemoveURL: "/user2/repo1/settings/asc/refs/remove",
		Base: "ab12", Op: "op-1", LandWords: asctrust.LandWords,
		Refs: []ascRefRow{
			{RefRule: asctrust.RefRule{Pattern: "release/*", Land: []string{asctrust.LandQueue, asctrust.LandMaintainers}, Signed: true}, EditURL: ascRefEditLink(rules, "release/*")},
			{RefRule: asctrust.RefRule{Pattern: "refs/tags/v*", Land: []string{asctrust.LandReleaseKey}, Force: true, Delete: true}, EditURL: ascRefEditLink(rules, "refs/tags/v*")},
			{RefRule: asctrust.RefRule{Pattern: "frozen/*"}, EditURL: ascRefEditLink(rules, "frozen/*")},
		},
	}
}

// Board RepoSettingsRules, "Protected refs": trunk first, then one row per `[refs]` rule with its kind, who can
// land, force push and signing, and Edit and Remove.
func TestAscRefRulesTable(t *testing.T) {
	html := renderSettingsPage(t, t.TempDir(), "rules-refs", tplBranches, map[string]any{"AscRules": refRulesPage()})

	assert.Contains(t, html, `data-testid="asc-refs-add">Add rule</a>`)
	assert.Contains(t, html, `href="/user2/repo1/settings/branches?ref_add=1"`)
	trunk := html[strings.Index(html, `data-testid="asc-refs-trunk"`):]
	trunk = trunk[:strings.Index(trunk, "</tr>")]
	assert.Contains(t, trunk, `<td class="ds-mono">main</td>`)
	assert.Contains(t, trunk, "<td>Trunk</td>")
	assert.Contains(t, trunk, "<td>Merge queue</td>", "queue only: no direct push")
	assert.Contains(t, trunk, `href="/user2/repo1/settings/asc/policy"`, "trunk is edited on the policy page")
	assert.Less(t, strings.Index(html, `asc-refs-trunk`), strings.Index(html, `data-pattern="release/*"`))

	assert.Contains(t, html, `<td class="ds-mono">release/*</td>`)
	assert.Contains(t, html, "<td>Release lines</td>")
	assert.Contains(t, html, "<td>Merge queue, Maintainers</td>")
	assert.Contains(t, html, `<td class="ds-mono">v*</td>`, "tags are shown without refs/tags/")
	assert.Contains(t, html, "<td>Tags</td>")
	assert.Contains(t, html, "<td>Release key (CI)</td>")
	assert.Contains(t, html, "<td>Allowed · can be deleted</td>")
	assert.Contains(t, html, "<td>Nobody</td>", "an empty land list")
	assert.Equal(t, 3, strings.Count(html, "<td>Not required</td>"), "trunk and two rules")
	assert.Contains(t, html, `href="/user2/repo1/settings/branches?ref_edit=refs%2Ftags%2Fv%2A"`)
	assert.Contains(t, html, `<input type="hidden" name="pattern" value="refs/tags/v*">`)
	assert.Contains(t, html, `aria-label="Remove the rule for v*"`)
	assert.Equal(t, 3, strings.Count(html, `action="/user2/repo1/settings/asc/refs/remove"`))
	assert.Contains(t, html, `<input type="hidden" name="base" value="ab12">`)
	assert.NotContains(t, html, `data-testid="asc-refs-panel"`, "no panel unless asked")
	assert.NotContains(t, html, "Only trunk is protected")

	only := refRulesPage()
	only.Refs = nil
	only.Protection.QueueOnly = false
	html = renderSettingsPage(t, t.TempDir(), "rules-refs-trunk", tplBranches, map[string]any{"AscRules": only})
	assert.Contains(t, html, "Only trunk is protected. Add a rule for release lines or tags.")
	assert.Contains(t, html, "<td>Merge queue, Writers</td>")

	broken := refRulesPage()
	broken.Problem = "The policy could not be read right now."
	html = renderSettingsPage(t, t.TempDir(), "rules-refs-problem", tplBranches, map[string]any{"AscRules": broken})
	assert.NotContains(t, html, `data-testid="asc-refs"`, "no table over a policy that could not be read")
}

// Board RepoSettingsRules, the side panel: kind, pattern, who can land as checkboxes, three switches and the
// problem of a refused save.
func TestAscRefRulesPanel(t *testing.T) {
	page := refRulesPage()
	r := page.Refs[1].RefRule
	page.Panel = &ascRefPanel{Was: r.Pattern, Tags: true, Name: r.Name(), Rule: r, Problem: "a rule with this pattern exists"}
	html := renderSettingsPage(t, t.TempDir(), "rules-refs-panel", tplBranches, map[string]any{"AscRules": page})

	assert.Contains(t, html, `class="ds-card ds-card-attach asc-refs asc-refs-open"`)
	assert.Contains(t, html, `<h5 id="asc-refs-panel-title">Edit the rule</h5>`)
	assert.Contains(t, html, `role="alert">a rule with this pattern exists</div>`)
	assert.Contains(t, html, `<input type="hidden" name="was" value="refs/tags/v*">`)
	assert.Contains(t, html, `<option value="tags" selected>Tags</option>`)
	assert.Contains(t, html, `<option value="branches">Release lines</option>`)
	assert.Contains(t, html, `name="name" required maxlength="256" value="v*"`)
	assert.Contains(t, html, `name="land" value="release-key" checked>`)
	assert.Contains(t, html, `name="land" value="queue">`)
	assert.Contains(t, html, "Changes an agent wrote, through the queue")
	assert.Contains(t, html, `<input type="checkbox" role="switch" name="signed"><span`)
	assert.Contains(t, html, `<input type="checkbox" role="switch" name="force" checked>`)
	assert.Contains(t, html, `<input type="checkbox" role="switch" name="delete" checked>`)
	assert.Contains(t, html, "Require signed changes")
	assert.Contains(t, html, "Allow force push")
	assert.Contains(t, html, "Allow deleting")
	assert.Contains(t, html, `href="/user2/repo1/settings/branches">Close</a>`)
	assert.Contains(t, html, `action="/user2/repo1/settings/asc/refs"`)

	page.Panel = &ascRefPanel{Rule: asctrust.RefRule{Land: []string{asctrust.LandQueue}}}
	html = renderSettingsPage(t, t.TempDir(), "rules-refs-add", tplBranches, map[string]any{"AscRules": page})
	assert.Contains(t, html, `<h5 id="asc-refs-panel-title">Add a rule</h5>`)
	assert.Contains(t, html, `<option value="branches" selected>Release lines</option>`)
	assert.NotContains(t, html, `role="alert"`)
}

func TestAscRefPanelOf(t *testing.T) {
	open := func(query string) *ascRefPanel {
		ctx, _ := secretsReq(t, "GET", 2, perm.AccessModeAdmin, nil)
		ctx.Req.Form, _ = url.ParseQuery(query)
		page := refRulesPage()
		ascRefPanelOf(ctx, page)
		return page.Panel
	}
	assert.Nil(t, open(""))
	assert.Nil(t, open("ref_edit=unknown"), "a rule the policy does not have opens nothing")
	add := open("ref_add=1")
	require.NotNil(t, add)
	assert.Empty(t, add.Was)
	assert.Equal(t, []string{asctrust.LandQueue, asctrust.LandMaintainers}, add.Rule.Land, "a new rule starts with the queue and maintainers")
	edit := open("ref_edit=refs%2Ftags%2Fv%2A")
	require.NotNil(t, edit)
	assert.Equal(t, "refs/tags/v*", edit.Was)
	assert.True(t, edit.Tags)
	assert.Equal(t, "v*", edit.Name)
	assert.True(t, edit.Rule.Force)

	ctx, _ := secretsReq(t, "GET", 2, perm.AccessModeAdmin, nil)
	ctx.Req.Form = url.Values{"ref_add": {"1"}}
	posted := &ascRefPanel{Name: "typed", Problem: "refused"}
	ctx.Data["AscRefPanelPosted"] = posted
	page := refRulesPage()
	ascRefPanelOf(ctx, page)
	assert.Same(t, posted, page.Panel, "a refused form is shown as typed")
}

func TestRefRuleOfForm(t *testing.T) {
	read := func(form url.Values) *ascRefPanel {
		ctx, _ := secretsReq(t, "POST", 2, perm.AccessModeAdmin, form)
		return refRuleOfForm(ctx)
	}
	p := read(url.Values{"was": {" release/* "}, "kind": {"branches"}, "name": {" refs/heads/stable/* "}, "land": {"queue", "agents"}, "signed": {"on"}})
	assert.Equal(t, "release/*", p.Was)
	assert.False(t, p.Tags)
	assert.Equal(t, asctrust.RefRule{Pattern: "stable/*", Land: []string{"queue", "agents"}, Signed: true}, p.Rule, "refs/heads/ is dropped")

	p = read(url.Values{"kind": {"tags"}, "name": {"v*"}, "force": {"on"}, "delete": {"on"}})
	assert.True(t, p.Tags)
	assert.Equal(t, asctrust.RefRule{Pattern: "refs/tags/v*", Force: true, Delete: true}, p.Rule)
	p = read(url.Values{"kind": {"tags"}, "name": {"refs/tags/v*"}})
	assert.Equal(t, "refs/tags/v*", p.Rule.Pattern, "refs/tags/ is not doubled")
}

// Saving or removing a rule is an administrator's: anyone else is refused before the policy is read.
func TestAscRefRulesRefuseNonAdmins(t *testing.T) {
	defer test.MockVariableValue(&setting.Asc.Enabled, true)()
	for _, access := range []perm.AccessMode{perm.AccessModeRead, perm.AccessModeWrite} {
		ctx, rec := secretsReq(t, "POST", 4, access, url.Values{"kind": {"branches"}, "name": {"release/*"}, "land": {"queue"}})
		AscRefRuleSave(ctx)
		assert.Equal(t, http.StatusForbidden, rec.Code)
		ctx, rec = secretsReq(t, "POST", 4, access, url.Values{"pattern": {"release/*"}})
		AscRefRuleRemove(ctx)
		assert.Equal(t, http.StatusForbidden, rec.Code)
	}
}
