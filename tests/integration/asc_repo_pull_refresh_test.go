// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package integration

import (
	"net/http"
	"testing"

	"forgejo.org/tests"

	"github.com/stretchr/testify/assert"
)

// TestAscRepoPullRefresh checks the phone repository home (board Mobile-Repo): pull to refresh over the body, which
// asc-pull-refresh.js runs in the installed app, next to the sticky Clone and New stack buttons.
func TestAscRepoPullRefresh(t *testing.T) {
	defer tests.PrepareTestEnv(t)()

	for _, path := range []string{"/user2/repo1", "/user2/repo1/src/branch/master/"} {
		page := NewHTMLParser(t, MakeRequest(t, NewRequest(t, "GET", path), http.StatusOK).Body)
		page.AssertElement(t, ".rp-page > .ds-ptr[data-asc-ptr][aria-hidden=true]", true)
		page.AssertElement(t, ".rp-body[data-asc-pull-refresh]", true)
	}
	page := NewHTMLParser(t, MakeRequest(t, NewRequest(t, "GET", "/user2/repo1"), http.StatusOK).Body)
	page.AssertElement(t, ".rp-sticky-m", true)
}

// TestAscRepoStickyNewStack checks the second sticky button of the phone repository home: a signed-in user who can
// open a stack gets New pull request (a git repository) to the compare page; a visitor gets the file history instead.
func TestAscRepoStickyNewStack(t *testing.T) {
	defer tests.PrepareTestEnv(t)()

	session := loginUser(t, "user2")
	page := NewHTMLParser(t, session.MakeRequest(t, NewRequest(t, "GET", "/user2/repo1"), http.StatusOK).Body)
	link := page.Find(".rp-sticky-m [data-testid=sticky-new-stack]")
	assert.Equal(t, 1, link.Length())
	href, _ := link.Attr("href")
	assert.Equal(t, "/user2/repo1/compare/master...master", href)
	assert.Contains(t, link.Text(), "New pull request")

	anon := NewHTMLParser(t, MakeRequest(t, NewRequest(t, "GET", "/user2/repo1"), http.StatusOK).Body)
	anon.AssertElement(t, ".rp-sticky-m [data-testid=sticky-new-stack]", false)
	anon.AssertElement(t, ".rp-sticky-m a[href='/user2/repo1/commits/branch/master']", true)
}
