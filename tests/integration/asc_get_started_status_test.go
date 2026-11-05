// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package integration

import (
	"encoding/json"
	"net/http"
	"testing"

	auth_model "forgejo.org/models/auth"
	"forgejo.org/models/db"
	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"
	"forgejo.org/tests"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type ascStartedStatus struct {
	Connected bool `json:"connected"`
	Done      int  `json:"done"`
	Total     int  `json:"total"`
}

func getStartedStatus(t *testing.T, session *TestSession) ascStartedStatus {
	t.Helper()
	resp := session.MakeRequest(t, NewRequest(t, "GET", "/-/get-started/status"), http.StatusOK)
	assert.Equal(t, "no-store", resp.Header().Get("Cache-Control"))
	var got ascStartedStatus
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &got))
	return got
}

// TestAscGetStartedStatus checks that step 2 of Get started (board GetStarted) can tick by itself: the page waits for
// the first sign-in of the command line, the status route reports it once an access token exists, and the page then
// stops waiting.
func TestAscGetStartedStatus(t *testing.T) {
	defer tests.PrepareTestEnv(t)()

	user8 := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 8})
	_, err := db.GetEngine(db.DefaultContext).Where("uid = ?", user8.ID).Delete(&auth_model.AccessToken{})
	require.NoError(t, err)
	session := loginUser(t, user8.Name)

	before := getStartedStatus(t, session)
	assert.False(t, before.Connected)
	assert.Equal(t, 4, before.Total)

	page := NewHTMLParser(t, session.MakeRequest(t, NewRequest(t, "GET", "/-/get-started"), http.StatusOK).Body)
	watch, ok := page.Find(".site-started").Attr("data-gs-watch")
	assert.True(t, ok)
	assert.Equal(t, "/-/get-started/status", watch)
	for _, part := range []string{"top", "steps", "side", "health"} {
		page.AssertElement(t, "[data-gs-part='"+part+"']", true)
	}
	live, _ := page.Find("[data-gs-live]").Attr("data-t-connected")
	assert.Equal(t, "The command line signed in. Step 2 is done.", live)
	page.AssertElement(t, ".site-steps > li:nth-child(2).is-current", true)

	// the command line signs in: it creates an access token
	require.NoError(t, auth_model.NewAccessToken(db.DefaultContext, &auth_model.AccessToken{UID: user8.ID, Name: "asc-cli", Scope: auth_model.AccessTokenScopeAll}))

	after := getStartedStatus(t, session)
	assert.True(t, after.Connected)
	assert.Equal(t, before.Done+2, after.Done)

	page = NewHTMLParser(t, session.MakeRequest(t, NewRequest(t, "GET", "/-/get-started"), http.StatusOK).Body)
	_, ok = page.Find(".site-started").Attr("data-gs-watch")
	assert.False(t, ok)
	page.AssertElement(t, ".site-steps > li:nth-child(2).is-done", true)
	page.AssertElement(t, ".site-steps > li:nth-child(3).is-current", true)

	t.Run("NeedsASignIn", func(t *testing.T) {
		resp := emptyTestSession(t).MakeRequest(t, NewRequest(t, "GET", "/-/get-started/status"), http.StatusSeeOther)
		assert.Contains(t, resp.Header().Get("Location"), "/user/login")
	})
}
