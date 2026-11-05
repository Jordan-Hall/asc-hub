// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package integration

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"forgejo.org/modules/setting"
	"forgejo.org/modules/test"
	"forgejo.org/tests"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAscExploreUsersHead checks the head of the people page (board ExploreUsers): the People and agents title with
// its lead, and Invite people, which adds an account for an administrator and sends the sign-up link by email for
// anyone else where sign-up is open.
func TestAscExploreUsersHead(t *testing.T) {
	defer tests.PrepareTestEnv(t)()

	invite := func(t *testing.T, session *TestSession) (string, bool) {
		t.Helper()
		page := NewHTMLParser(t, session.MakeRequest(t, NewRequest(t, "GET", "/explore/users"), http.StatusOK).Body)
		head := page.Find(".ds-page-head")
		assert.Equal(t, "People and agents", strings.TrimSpace(head.Find("h1 > span").Last().Text()))
		assert.Equal(t, "The people and agents who write and review here, with how much they do.", strings.TrimSpace(head.Find(".ds-page-desc").Text()))
		btn := head.Find(".ds-page-actions a.ds-btn-primary")
		if btn.Length() == 0 {
			return "", false
		}
		assert.Equal(t, "Invite people", strings.TrimSpace(btn.Text()))
		return btn.AttrOr("href", ""), true
	}

	t.Run("signed out", func(t *testing.T) {
		_, ok := invite(t, emptyTestSession(t))
		assert.False(t, ok)
	})

	t.Run("administrator", func(t *testing.T) {
		href, ok := invite(t, loginUser(t, "user1"))
		require.True(t, ok)
		assert.Equal(t, "/admin/users/new", href)
	})

	t.Run("user with open sign-up", func(t *testing.T) {
		defer test.MockVariableValue(&setting.Service.DisableRegistration, false)()
		href, ok := invite(t, loginUser(t, "user2"))
		require.True(t, ok)
		require.True(t, strings.HasPrefix(href, "mailto:?subject="), href)
		assert.NotContains(t, href, "+", "a space is %20 in a mailto link")
		query, err := url.ParseQuery(strings.TrimPrefix(href, "mailto:?"))
		require.NoError(t, err)
		assert.Equal(t, "Join me on "+setting.AppName, query.Get("subject"))
		assert.Contains(t, query.Get("body"), setting.AppURL+"user/sign_up")
	})

	t.Run("user with sign-up closed", func(t *testing.T) {
		defer test.MockVariableValue(&setting.Service.DisableRegistration, true)()
		_, ok := invite(t, loginUser(t, "user2"))
		assert.False(t, ok)
	})
}
