// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package integration

import (
	"net/http"
	"testing"

	"forgejo.org/modules/setting"
	"forgejo.org/modules/test"
	"forgejo.org/tests"

	"github.com/stretchr/testify/assert"
)

// Row-level visibility is the platform's on every list that crosses repositories: a private repository, a
// repository of a private organization, a private organization and a private profile show on no list for a viewer
// the platform would not show them to, signed out or signed in without access; the owner sees them, so the
// lists are not empty by accident. The repository matrix of asc's authz answer is routers/private/asc_parity_test.go.
func TestAscRowVisibilityMatrix(t *testing.T) {
	defer tests.PrepareTestEnv(t)()
	defer test.MockVariableValue(&setting.Asc.Enabled, true)()

	type viewer struct {
		name string
		get  func(t *testing.T, url string, status int) string
	}
	anon := viewer{"signed out", func(t *testing.T, url string, status int) string {
		return MakeRequest(t, NewRequest(t, "GET", url), status).Body.String()
	}}
	stranger := loginUser(t, "user8")
	signedIn := viewer{"signed in without access", func(t *testing.T, url string, status int) string {
		return stranger.MakeRequest(t, NewRequest(t, "GET", url), status).Body.String()
	}}
	owner := loginUser(t, "user2")

	// Lists that cross repositories, and the private names that must not be on them.
	lists := []struct{ url, hidden string }{
		{"/explore/repos?q=repo2", `href="/user2/repo2"`},
		{"/explore/repos?q=repo3", `href="/org3/repo3"`},
		{"/api/v1/repos/search?q=repo2&limit=50", `"full_name":"user2/repo2"`},
		{"/api/v1/repos/search?q=repo3&limit=50", `"full_name":"org3/repo3"`},
		{"/user2?tab=repositories&q=repo2", `href="/user2/repo2"`},
		{"/user2?tab=activity", `href="/user2/repo2"`},
		{"/org3", `href="/org3/repo3"`},
		{"/-/asc/palette?q=repo2", `"href":"/user2/repo2"`},
		{"/explore/organizations?q=privated", `href="/privated_org"`},
		{"/explore/users?q=user31", `href="/user31"`},
		{"/api/v1/users/search?q=user31", `"login":"user31"`},
	}
	for _, v := range []viewer{anon, signedIn} {
		for _, l := range lists {
			body := v.get(t, l.url, http.StatusOK)
			assert.NotContains(t, body, l.hidden, "%s: %s", v.name, l.url)
		}
		v.get(t, "/user2/repo2", http.StatusNotFound)
		v.get(t, "/org3/repo3", http.StatusNotFound)
		v.get(t, "/privated_org", http.StatusNotFound)
	}
	anon.get(t, "/user31", http.StatusNotFound)
	anon.get(t, "/limited_org", http.StatusNotFound)
	signedIn.get(t, "/limited_org", http.StatusOK)

	// The signed-in lists: the dashboard feed, the inbox and the notifications; the admin lists are the admin's.
	for _, url := range []string{"/", "/-/asc/inbox", "/notifications"} {
		body := signedIn.get(t, url, http.StatusOK)
		assert.NotContains(t, body, `href="/user2/repo2"`, url)
		assert.NotContains(t, body, `href="/org3/repo3"`, url)
	}
	for _, url := range []string{"/admin/repos", "/admin/users", "/admin/orgs"} {
		stranger.MakeRequest(t, NewRequest(t, "GET", url), http.StatusForbidden)
		MakeRequest(t, NewRequest(t, "GET", url), http.StatusSeeOther)
	}

	token := getUserToken(t, "user2", "read:repository")
	apiBody := MakeRequest(t, NewRequest(t, "GET", "/api/v1/repos/search?q=repo2&limit=50").AddTokenAuth(token), http.StatusOK).Body.String()
	assert.Contains(t, apiBody, `"full_name":"user2/repo2"`, "the owner: the API search")
	for _, url := range []string{"/explore/repos?q=repo2", "/user2?tab=repositories&q=repo2"} {
		body := owner.MakeRequest(t, NewRequest(t, "GET", url), http.StatusOK).Body.String()
		assert.Contains(t, body, `href="/user2/repo2"`, "the owner: %s", url)
	}
}
