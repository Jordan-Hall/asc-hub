// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package integration

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Board Onboarding: an OpenID Connect source is the organisation's single sign-on and says so with its name; a
// named provider keeps "Sign in with".
func TestSignInSingleSignOnLabel(t *testing.T) {
	onApplicationRun(t, func(t *testing.T, u *url.URL) {
		addAuthSource(t, authSourcePayloadOpenIDConnect("Acme SSO", u.String()))
		addAuthSource(t, authSourcePayloadGitHub("github"))

		doc := NewHTMLParser(t, MakeRequest(t, NewRequest(t, "GET", "/user/login"), http.StatusOK).Body)
		links := doc.Find("#oauth2-login-navigator a.oauth-login-link")
		labels := map[string]string{}
		links.Each(func(_ int, a *goquery.Selection) {
			href, _ := a.Attr("href")
			labels[href] = strings.TrimSpace(a.Text())
		})
		require.Len(t, labels, 2)
		assert.Equal(t, "Single sign-on (Acme SSO)", labels["/user/oauth2/Acme%20SSO"])
		assert.Equal(t, "Sign in with github", labels["/user/oauth2/github"])
	})
}
