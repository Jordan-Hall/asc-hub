// Copyright 2026 The Forgejo Authors
// SPDX-License-Identifier: GPL-3.0-or-later

package integration

import (
	"net/http"
	"strings"
	"testing"

	"forgejo.org/modules/setting"
	"forgejo.org/modules/test"
	"forgejo.org/tests"

	"github.com/stretchr/testify/assert"
)

/* TestFooterContent asserts go tmpl logic of footer */
func TestFooterContent(t *testing.T) {
	defer tests.PrepareTestEnv(t)()

	// The footer can be tested on any page, but preferably a lightweight one
	regularPage := "/explore/organizations"
	adminPage := "/admin"

	adminUser := loginUser(t, "user1")
	regularUser := loginUser(t, "user2")

	t.Run(`Default configuration`, func(t *testing.T) {
		defer tests.PrintCurrentTest(t)()

		// The version links to /admin/config and is shown to site admins only;
		// signed-out visitors and regular users see neither it nor the render time
		testFooterContent(t, emptyTestSession(t), regularPage, true, false, false)
		testFooterContent(t, regularUser, regularPage, true, false, false)
		testFooterContent(t, adminUser, regularPage, true, true, true)
		testFooterContent(t, adminUser, adminPage, true, true, true)

		// The page render time is for site admins only too
		for _, c := range []struct {
			session *TestSession
			want    bool
		}{{emptyTestSession(t), false}, {regularUser, false}, {adminUser, true}} {
			page := NewHTMLParser(t, c.session.MakeRequest(t, NewRequest(t, "GET", regularPage), http.StatusOK).Body)
			page.AssertElement(t, `footer [data-testid="footer-rendered"]`, c.want)
		}
	})

	t.Run(`ShowFooterPoweredBy is disabled`, func(t *testing.T) {
		defer tests.PrintCurrentTest(t)()
		defer test.MockVariableValue(&setting.Other.ShowFooterPoweredBy, false)()

		testFooterContent(t, regularUser, regularPage, false, false, false)
		testFooterContent(t, adminUser, regularPage, false, true, true)
	})

	t.Run(`ShowFooterVersion is disabled`, func(t *testing.T) {
		defer tests.PrintCurrentTest(t)()
		defer test.MockVariableValue(&setting.Other.ShowFooterVersion, false)()

		// ShowFooterVersion is on by default. Disabling it hides the version on
		// all pages but not on /admin
		testFooterContent(t, regularUser, regularPage, true, false, false)
		testFooterContent(t, adminUser, regularPage, true, false, false)
		testFooterContent(t, adminUser, adminPage, true, true, true)
	})
}

// When visiting certain pages, the corresponding entry of user menu is highlighted
func testFooterContent(t *testing.T, session *TestSession, url string, expectPoweredBy, expectVersion, expectVersionLink bool) {
	page := NewHTMLParser(t, session.MakeRequest(t, NewRequest(t, "GET", url), http.StatusOK).Body)
	// AssertElement will only pass if there's just one such element
	footerBase := page.Find("footer .ds-footer-base").Text()
	assert.Equal(t, expectPoweredBy, strings.Contains(strings.ToLower(footerBase), "based on forgejo"))
	assert.Equal(t, expectVersion, strings.Contains(footerBase, strings.Split(setting.AppVer, "+")[0]))
	page.AssertElement(t, `footer .ds-footer-base a[href="/admin/config"]`, expectVersionLink)
}
