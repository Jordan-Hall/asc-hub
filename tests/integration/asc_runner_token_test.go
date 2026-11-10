// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package integration

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	actions_model "forgejo.org/models/actions"
	"forgejo.org/models/db"
	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/optional"
	"forgejo.org/modules/timeutil"

	runnerv1 "code.forgejo.org/forgejo/actions-proto/runner/v1"
	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The runners page shows a registration token once, right after it is asked for: it expires after an hour, a reload does
// not show it again, and an expired token registers no runner.
func TestRunnerRegistrationTokenShownOnce(t *testing.T) {
	onApplicationRun(t, func(t *testing.T, _ *url.URL) { testRunnerRegistrationTokenShownOnce(t) })
}

// the mock runner registers over the network, so the server listens while this runs
func testRunnerRegistrationTokenShownOnce(t *testing.T) {
	admin := unittest.AssertExistsAndLoadBean(t, &user_model.User{IsAdmin: true})
	session := loginUser(t, admin.Name)
	const page = "/admin/actions/runners"
	get := func() *HTMLDoc {
		return NewHTMLParser(t, session.MakeRequest(t, NewRequest(t, "GET", page), http.StatusOK).Body)
	}

	doc := get()
	doc.AssertElement(t, `[data-testid="runner-token-once"]`, false)
	doc.AssertElement(t, `.adm-reg .ds-term`, false)
	assert.Equal(t, "Get a registration token", strings.TrimSpace(doc.Find(`[data-testid="runner-token-get"]`).Text()))

	before := time.Now()
	resp := session.MakeRequest(t, NewRequest(t, "POST", page+"/reset_registration_token"), http.StatusSeeOther)
	assert.Equal(t, page, strings.TrimSuffix(resp.Header().Get("Location"), "/"))
	token, err := actions_model.GetLatestRunnerToken(db.DefaultContext, optional.None[int64](), optional.None[int64]())
	require.NoError(t, err)
	assert.True(t, token.Usable())
	assert.WithinDuration(t, before.Add(time.Hour), token.ExpiresUnix.AsTime(), time.Minute)

	doc = get()
	doc.AssertElement(t, `[data-testid="runner-token-once"]`, true)
	assert.Contains(t, doc.Find(`.adm-reg .ds-term-bd`).Text(), "--token "+token.Token)
	assert.Equal(t, "Reset the token", strings.TrimSpace(doc.Find(`[data-testid="runner-token-get"]`).Text()))

	doc = get()
	doc.AssertElement(t, `[data-testid="runner-token-once"]`, false)
	assert.NotContains(t, doc.Find(`.adm-reg`).Text(), token.Token, "a reload does not show the token again")

	// a token asked for on one page is not shown on the page of another scope
	session.MakeRequest(t, NewRequest(t, "POST", page+"/reset_registration_token"), http.StatusSeeOther)
	body := session.MakeRequest(t, NewRequest(t, "GET", "/user/settings/actions/runners"), http.StatusOK).Body.String()
	assert.NotContains(t, body, `data-testid="runner-token-once"`)
	get().AssertElement(t, `[data-testid="runner-token-once"]`, false)

	// the user's own page shows its token once in the menu
	session.MakeRequest(t, NewRequest(t, "POST", "/user/settings/actions/runners/reset_registration_token"), http.StatusSeeOther)
	userToken, err := actions_model.GetLatestRunnerToken(db.DefaultContext, optional.Some(admin.ID), optional.None[int64]())
	require.NoError(t, err)
	doc = NewHTMLParser(t, session.MakeRequest(t, NewRequest(t, "GET", "/user/settings/actions/runners"), http.StatusOK).Body)
	doc.AssertElement(t, `[data-testid="runner-token-once"]`, true)
	assert.Equal(t, userToken.Token, doc.Find(`.runner-container input[readonly]`).AttrOr("value", ""))

	// the token registers a runner while it lasts, and none once it expired
	token, err = actions_model.GetLatestRunnerToken(db.DefaultContext, optional.None[int64](), optional.None[int64]())
	require.NoError(t, err)
	runner := newMockRunner()
	runner.doRegister(t, "nova-runner", token.Token, []string{"ubuntu"})

	token.ExpiresUnix = timeutil.TimeStampNow() - 1
	require.NoError(t, actions_model.UpdateRunnerToken(db.DefaultContext, token, "expires_unix"))
	late := newMockRunner()
	late.doPing(t)
	_, err = late.client.runnerServiceClient.Register(t.Context(), connect.NewRequest(&runnerv1.RegisterRequest{
		Name: "orbit-runner", Token: token.Token, Version: "mock-runner-version", Labels: []string{"ubuntu"},
	}))
	require.Error(t, err)
	assert.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
	assert.Contains(t, err.Error(), "expired")
}
