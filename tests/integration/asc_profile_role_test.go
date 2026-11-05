// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package integration

import (
	"net/http"
	"strings"
	"testing"

	"forgejo.org/tests"

	"github.com/stretchr/testify/assert"
)

// TestAscProfileRole checks the role line of a profile (boards Profile and Mobile-Profile): "@priya · Staff engineer ·
// Payments team · London". The role is a profile setting; the team is the first team the viewer may know about.
func TestAscProfileRole(t *testing.T) {
	defer tests.PrepareTestEnv(t)()
	session := loginUser(t, "user2")
	guest := emptyTestSession(t)

	setRole := func(role string, status int) {
		t.Helper()
		session.MakeRequest(t, NewRequestWithValues(t, "POST", "/user/settings", map[string]string{"role": role}), status)
	}

	// no role yet: no role part
	page := NewHTMLParser(t, session.MakeRequest(t, NewRequest(t, "GET", "/user2"), http.StatusOK).Body)
	page.AssertElement(t, "[data-asc-pf-role]", false)

	setRole("  Staff engineer  ", http.StatusSeeOther)

	settings := NewHTMLParser(t, session.MakeRequest(t, NewRequest(t, "GET", "/user/settings"), http.StatusOK).Body)
	value, _ := settings.Find("input#role").Attr("value")
	assert.Equal(t, "Staff engineer", value, "saved trimmed and shown in settings")

	page = NewHTMLParser(t, session.MakeRequest(t, NewRequest(t, "GET", "/user2"), http.StatusOK).Body)
	assert.Equal(t, "Staff engineer", strings.TrimSpace(page.Find(".wk-pf-line2 [data-asc-pf-role]").Text()))
	team := strings.TrimSpace(page.Find(".wk-pf-line2 [data-asc-pf-team]").Text())
	assert.True(t, strings.HasSuffix(team, " team"), "the person sees their own first team: %q", team)

	// a visitor sees the role, not the teams
	page = NewHTMLParser(t, guest.MakeRequest(t, NewRequest(t, "GET", "/user2"), http.StatusOK).Body)
	assert.Equal(t, "Staff engineer", strings.TrimSpace(page.Find("[data-asc-pf-role]").Text()))
	page.AssertElement(t, "[data-asc-pf-team]", false)

	// too long: refused, the old role stays
	setRole(strings.Repeat("x", 61), http.StatusOK)
	page = NewHTMLParser(t, guest.MakeRequest(t, NewRequest(t, "GET", "/user2"), http.StatusOK).Body)
	assert.Equal(t, "Staff engineer", strings.TrimSpace(page.Find("[data-asc-pf-role]").Text()))

	// cleared: gone
	setRole("", http.StatusSeeOther)
	page = NewHTMLParser(t, guest.MakeRequest(t, NewRequest(t, "GET", "/user2"), http.StatusOK).Body)
	page.AssertElement(t, "[data-asc-pf-role]", false)
}
