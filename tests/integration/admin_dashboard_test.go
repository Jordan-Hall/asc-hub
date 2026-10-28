// Copyright 2025 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package integration

import (
	"fmt"
	"net/http"
	"testing"

	"forgejo.org/modules/setting"
	"forgejo.org/modules/test"
	"forgejo.org/modules/translation"
	"forgejo.org/tests"

	"github.com/stretchr/testify/assert"
)

var commonEntries = []string{
	"delete_inactive_accounts",
	"delete_repo_archives",
	"delete_missing_repos",
	"git_gc_repos",
	"reinit_missing_repos",
	"sync_external_users",
	"repo_health_check",
	"delete_generated_repository_avatars",
	"sync_repo_branches",
	"sync_repo_tags",
}

var sshEntries = []string{
	"resync_all_sshkeys",
	"resync_all_sshprincipals",
}

// Check cron options on /admin, including those that are available conditionally
func testAssertAdminDashboardOptions(t *testing.T, page *HTMLDoc, locale translation.Locale, expectSSH bool) {
	for _, entry := range commonEntries {
		page.AssertSelection(t, page.FindByText(".ds-list .ds-li-title", locale.TrString(fmt.Sprintf("admin.dashboard.%s", entry))), true)
		page.AssertSelection(t, page.Find(fmt.Sprintf(".ds-list .ds-li button[value='%s']", entry)), true)
	}
	for _, entry := range sshEntries {
		page.AssertSelection(t, page.FindByText(".ds-list .ds-li-title", locale.TrString(fmt.Sprintf("admin.dashboard.%s", entry))), expectSSH)
		page.AssertSelection(t, page.Find(fmt.Sprintf(".ds-list .ds-li button[value='%s']", entry)), expectSSH)
	}
}

func TestAdminDashboard(t *testing.T) {
	defer tests.PrepareTestEnv(t)()

	session := loginUser(t, "user1")
	locale := translation.NewLocale("en-US")
	url := "/admin"

	t.Run("SSH disabled", func(t *testing.T) {
		defer tests.PrintCurrentTest(t)()
		defer test.MockVariableValue(&setting.SSH.Disabled, true)()

		page := NewHTMLParser(t, session.MakeRequest(t, NewRequest(t, "GET", url), http.StatusOK).Body)
		testAssertAdminDashboardOptions(t, page, locale, false)
	})

	t.Run("SSH enabled, but built-in", func(t *testing.T) {
		defer tests.PrintCurrentTest(t)()
		defer test.MockVariableValue(&setting.SSH.Disabled, false)()
		defer test.MockVariableValue(&setting.SSH.StartBuiltinServer, true)()

		page := NewHTMLParser(t, session.MakeRequest(t, NewRequest(t, "GET", url), http.StatusOK).Body)
		testAssertAdminDashboardOptions(t, page, locale, false)
	})

	t.Run("SSH enabled and external", func(t *testing.T) {
		defer tests.PrintCurrentTest(t)()
		defer test.MockVariableValue(&setting.SSH.Disabled, false)()
		defer test.MockVariableValue(&setting.SSH.StartBuiltinServer, false)()

		page := NewHTMLParser(t, session.MakeRequest(t, NewRequest(t, "GET", url), http.StatusOK).Body)
		testAssertAdminDashboardOptions(t, page, locale, true)
	})

	t.Run("System status", func(t *testing.T) {
		defer tests.PrintCurrentTest(t)()

		// Check data units translations in the System status table
		// (the figures moved to the health page)
		selector := "div[hx-get='/admin/system_status'] dl > div > dd"
		healthURL := "/admin/self_check"
		// ...in English
		page := NewHTMLParser(t, session.MakeRequest(t, NewRequest(t, "GET", healthURL), http.StatusOK).Body)
		assert.Contains(t, page.Find(selector).Text(), "MiB")

		// ...in another language
		lang := session.GetCookie("lang")
		lang.Value = "ru-RU"
		session.SetCookie(lang)

		page = NewHTMLParser(t, session.MakeRequest(t, NewRequest(t, "GET", healthURL), http.StatusOK).Body)
		assert.Contains(t, page.Find(selector).Text(), "МиБ")
	})
}
