// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package integration

import (
	"net/http"
	"strings"
	"testing"

	auth_model "forgejo.org/models/auth"
	"forgejo.org/modules/setting"
	api "forgejo.org/modules/structs"
	"forgejo.org/modules/test"
	"forgejo.org/tests"

	"github.com/stretchr/testify/assert"
)

// TestAscGetStartedDoctor checks the health check of Get started (board GetStarted): with no report the card asks
// for `asc doctor --report`; once the command line sends one, the card shows the level of Git and of the shim, the
// checks that warn or fail, and when the machine was checked. Only the token of the person may send it.
func TestAscGetStartedDoctor(t *testing.T) {
	defer tests.PrepareTestEnv(t)()
	defer test.MockVariableValue(&setting.Asc.Enabled, true)()
	session := loginUser(t, "user8")
	health := func() *HTMLDoc {
		return NewHTMLParser(t, session.MakeRequest(t, NewRequest(t, "GET", "/-/get-started"), http.StatusOK).Body)
	}

	page := health()
	assert.Contains(t, page.Find("[data-gs-check='git'] dd").Text(), "asc doctor --report")
	page.AssertElement(t, "[data-gs-problems]", false)
	assert.Contains(t, page.Find("[data-gs-reported]").Text(), "Run asc doctor --report on your machine")

	report := api.AscDoctorReport{
		Asc: "0.9.0", Git: "git version 2.47.1", OS: "linux", Arch: "x86_64",
		Checks: []api.AscDoctorCheck{
			{Name: "real-git", Level: "ok"},
			{Name: "shim-dir", Level: "ok"},
			{Name: "daemon", Level: "warn"},
			{Name: "credentials", Level: "fail"},
		},
	}
	MakeRequest(t, NewRequestWithJSON(t, "POST", "/api/v1/user/asc/doctor", &report), http.StatusUnauthorized)
	token := getTokenForLoggedInUser(t, session, auth_model.AccessTokenScopeWriteRepository)
	MakeRequest(t, NewRequestWithJSON(t, "POST", "/api/v1/user/asc/doctor", &report).AddTokenAuth(token), http.StatusNoContent)

	page = health()
	git := page.Find("[data-gs-check='git'] dd")
	assert.Contains(t, git.Text(), "OK")
	assert.Contains(t, git.Text(), "git version 2.47.1")
	assert.True(t, git.HasClass("ds-c-ok"))
	assert.True(t, page.Find("[data-gs-check='shim'] dd").HasClass("ds-c-ok"))
	problems := page.Find("[data-gs-problems] code")
	assert.Equal(t, 2, problems.Length())
	assert.Equal(t, "credentials", problems.First().Text(), "failures come first")
	assert.True(t, problems.First().HasClass("ds-c-err"))
	assert.Equal(t, "daemon", problems.Last().Text())
	assert.True(t, strings.HasPrefix(strings.TrimSpace(page.Find("[data-gs-reported]").Text()), "Checked on linux"))
	page.AssertElement(t, ".site-health-fix", true)

	report.Checks[0].Level = "perfect"
	MakeRequest(t, NewRequestWithJSON(t, "POST", "/api/v1/user/asc/doctor", &report).AddTokenAuth(token), http.StatusUnprocessableEntity)
	assert.True(t, health().Find("[data-gs-check='git'] dd").HasClass("ds-c-ok"), "a refused report keeps the last one")
}
