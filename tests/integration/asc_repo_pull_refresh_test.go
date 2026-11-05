// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package integration

import (
	"net/http"
	"testing"

	"forgejo.org/tests"
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
