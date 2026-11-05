// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package setting

import (
	gocontext "context"
	"net/http"
	"regexp"
	"testing"

	"forgejo.org/models/unit"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/test"
	"forgejo.org/modules/translation"
	"forgejo.org/services/context"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// menuData puts in ctx what the repository middleware gives every settings page.
func menuData(ctx *context.Context) {
	ctx.Data["AscEnabled"] = true
	ctx.Data["Repository"] = ctx.Repo.Repository
	ctx.Data["RepoLink"] = ctx.Repo.RepoLink
	ctx.Data["Permission"] = &ctx.Repo.Permission
	ctx.Data["UnitTypeCode"] = unit.TypeCode
	MenuStatus(ctx)
}

// renderMenu draws the settings menu of repository 1, stored as backend, for user2 (its owner) on the branchless page.
func renderMenu(t *testing.T, backend string) string {
	t.Helper()
	ctx, resp := branchlessCtx(t, 1, 2, backend)
	if len(setting.Langs) == 0 || setting.Langs[0] != "en-US" {
		setting.Langs, setting.Names = []string{"en-US"}, []string{"English"}
	}
	translation.InitLocales(gocontext.Background())
	ctx.Base.Locale = translation.NewLocale("en-US")
	ctx.Locale = ctx.Base.Locale
	ctx.TemplateContext = context.NewTemplateContextForWeb(ctx)
	menuData(ctx)
	ctx.Data["PageIsSettingsAscBranchless"] = true
	ctx.HTML(http.StatusOK, "repo/settings/navbar")
	require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
	return resp.Body.String()
}

var menuHref = regexp.MustCompile(`<li><a href="/user2/repo1/settings([^"]*)"`)

func TestAscSettingsMenu(t *testing.T) {
	defer test.MockVariableValue(&setting.Asc.Enabled, true)()

	t.Run("an asc repository has the board's groups and entries, in its order", func(t *testing.T) {
		menu := renderMenu(t, "asc")

		var hrefs []string
		for _, m := range menuHref.FindAllStringSubmatch(menu, -1) {
			hrefs = append(hrefs, m[1])
		}
		assert.Equal(t, []string{
			"#settings-general", "/collaboration", "/branches", "/asc/policy", "/asc/branchless", "/units", "/keys", "/tags",
			"/hooks",
			"/asc/storage", "/asc/ai",
			"/asc/security", "/asc/audit", "/asc/workflow-secrets", "/asc/path-rules",
		}, hrefs[:15])

		groups := regexp.MustCompile(`<h2 class="ds-settings-group">([^<]+)</h2>`).FindAllStringSubmatch(menu, -1)
		var names []string
		for _, g := range groups {
			names = append(names, g[1])
		}
		assert.Equal(t, []string{"Repository", "Automation", "Data", "Safety"}, names)

		assert.Contains(t, menu, `<li><a href="/user2/repo1/settings/branches" data-snav-desc="Protected branches and tags, required checks, required reviews, who can land">`)
		assert.Contains(t, menu, `<span class="ds-snav-label">Rules</span>`)
		assert.Contains(t, menu, `<span class="ds-snav-label">Policy</span>`)
		assert.Contains(t, menu, `<span class="ds-snav-label">Branches and stacks</span>`)
		assert.Contains(t, menu, `href="/user2/repo1/settings/asc/branchless" data-snav-desc="Branchless mode, stacks, landing, git access" aria-current="page"`)
		assert.Contains(t, menu, `data-snav-desc="Transfer, archive, delete, rename, ownership"`, "the danger zone is found by its words too")
		assert.Contains(t, menu, `data-snav-live-url="/user2/repo1/settings/asc/menu-status"`)
		assert.Contains(t, menu, `<span class="ds-snav-status" data-snav-live="storage" hidden></span>`)
		assert.Contains(t, menu, `<span class="ds-snav-status" data-snav-live="ai" hidden></span>`)
		assert.NotContains(t, menu, "asc_shell.", "every key is translated")
	})

	t.Run("a git repository keeps its own menu, without the asc groups or statuses", func(t *testing.T) {
		menu := renderMenu(t, "")
		assert.Contains(t, menu, `<h2 class="ds-settings-group">Code and access</h2>`)
		assert.NotContains(t, menu, "data-snav-live")
		assert.NotContains(t, menu, "/asc/storage")
		assert.NotContains(t, menu, `<h2 class="ds-settings-group">Safety</h2>`)
	})
}
