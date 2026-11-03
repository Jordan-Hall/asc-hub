// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package integration

import (
	"net/http"
	"testing"

	"forgejo.org/tests"

	"github.com/stretchr/testify/assert"
)

// Every settings layout (account, organization, repository, site administration) has the menu and the content the
// loading skeleton needs, with the words screen readers hear while the next page loads.
func TestSettingsPagesCarryTheLoadingWords(t *testing.T) {
	defer tests.PrepareTestEnv(t)()

	session := loginUser(t, "user1")
	for _, page := range []string{"/user/settings", "/user/settings/account", "/org/org3/settings", "/user2/repo1/settings", "/admin"} {
		doc := NewHTMLParser(t, session.MakeRequest(t, NewRequest(t, "GET", page), http.StatusOK).Body)
		doc.AssertElement(t, ".ds-settings-nav", true)
		assert.Equal(t, "Loading…", doc.Find(".ds-settings-body").AttrOr("data-loading", ""), page)
	}
}
