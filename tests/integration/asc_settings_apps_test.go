// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package integration

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	auth_model "forgejo.org/models/auth"
	"forgejo.org/models/db"
	"forgejo.org/models/unittest"
	"forgejo.org/models/webhook"
	"forgejo.org/modules/json"
	webhook_module "forgejo.org/modules/webhook"
	"forgejo.org/tests"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The board notes of UserSettingsApps: no applications is an empty state with New application, a client secret is
// shown once and can be regenerated, Replay on a failing webhook re-sends the last failed delivery, and Revoke removes
// the grant so every token issued under it stops working, with a message that says so.
func TestAscSettingsAppsBoardNotes(t *testing.T) {
	defer tests.PrepareTestEnv(t)()

	t.Run("empty, then a client secret shown once and regenerated", func(t *testing.T) {
		sess := loginUser(t, "user4")
		doc := NewHTMLParser(t, sess.MakeRequest(t, NewRequest(t, "GET", "/user/settings/applications"), http.StatusOK).Body)
		empty := doc.Find(`[data-testid="oauth2-apps-empty"]`)
		require.Equal(t, 1, empty.Length())
		assert.Contains(t, empty.Text(), "No OAuth2 applications")
		assert.Equal(t, "#application-name", empty.Find("a.ds-btn-primary").AttrOr("href", ""))
		assert.Contains(t, empty.Find("a.ds-btn-primary").Text(), "New application")
		assert.Equal(t, 1, doc.Find(`[data-testid="oauth2-app-form"] #application-name`).Length(), "the button leads to the form")

		resp := sess.MakeRequest(t, NewRequestWithValues(t, "POST", "/user/settings/applications/oauth2", map[string]string{
			"application_name": "board-app", "redirect_uris": "https://app.example.org/cb", "confidential_client": "on",
		}), http.StatusOK)
		app := unittest.AssertExistsAndLoadBean(t, &auth_model.OAuth2Application{Name: "board-app", UID: 4})
		secret := NewHTMLParser(t, resp.Body).Find("#client-secret")
		assert.Equal(t, "text", secret.AttrOr("type", ""), "the new secret is shown")
		first := secret.AttrOr("value", "")
		require.NotEmpty(t, first)

		link := fmt.Sprintf("/user/settings/applications/oauth2/%d", app.ID)
		again := NewHTMLParser(t, sess.MakeRequest(t, NewRequest(t, "GET", link), http.StatusOK).Body)
		assert.Equal(t, "password", again.Find("#client-secret").AttrOr("type", ""), "never again after that")
		assert.NotContains(t, again.Find("#client-secret").AttrOr("value", ""), first)

		resp = sess.MakeRequest(t, NewRequest(t, "POST", link+"/regenerate_secret"), http.StatusOK)
		second := NewHTMLParser(t, resp.Body).Find("#client-secret").AttrOr("value", "")
		require.NotEmpty(t, second)
		assert.NotEqual(t, first, second, "a new secret replaces the old one")
	})

	t.Run("failing webhook: Replay re-sends the last failed delivery", func(t *testing.T) {
		sess := loginUser(t, "user2")
		w := &webhook.Webhook{URL: "https://hooks.example.org/in", OwnerID: 2, IsActive: true, Type: webhook_module.FORGEJO}
		w.HookEvent = &webhook_module.HookEvent{SendEverything: true}
		w.ContentType = webhook.ContentTypeJSON
		require.NoError(t, w.UpdateEvent())
		require.NoError(t, db.Insert(db.DefaultContext, w))
		var last *webhook.HookTask
		for range 2 {
			task, err := webhook.CreateHookTask(db.DefaultContext, &webhook.HookTask{HookID: w.ID, PayloadVersion: 2, PayloadContent: "{}"})
			require.NoError(t, err)
			task.IsDelivered, task.IsSucceed = true, false
			task.ResponseInfo = &webhook.HookResponse{Status: http.StatusBadGateway}
			require.NoError(t, webhook.UpdateHookTask(db.DefaultContext, task))
			last = task
		}

		doc := NewHTMLParser(t, sess.MakeRequest(t, NewRequest(t, "GET", "/user/settings/hooks"), http.StatusOK).Body)
		action := fmt.Sprintf("/user/settings/hooks/%d/replay/%s", w.ID, last.UUID)
		form := doc.Find(`form[action="` + action + `"]`)
		require.Equal(t, 1, form.Length(), "the failing row offers Replay of its last delivery")

		before := unittest.GetCount(t, &webhook.HookTask{HookID: w.ID})
		resp := sess.MakeRequest(t, NewRequestWithValues(t, "POST", action, map[string]string{"redirect_to": "/user/settings/hooks"}), http.StatusSeeOther)
		assert.Equal(t, "/user/settings/hooks", resp.Header().Get("Location"))
		assert.Equal(t, before+1, unittest.GetCount(t, &webhook.HookTask{HookID: w.ID}), "one delivery is queued again")
	})

	t.Run("Revoke removes the grant and its tokens stop working", func(t *testing.T) {
		resp := MakeRequest(t, NewRequestWithValues(t, "POST", "/login/oauth/access_token", map[string]string{
			"grant_type": "authorization_code", "client_id": "da7da3ba-9a13-4167-856f-3899de0b0138",
			"client_secret": "4MK8Na6R55smdCY0WuCCumZ6hjRPnGY5saWVRHHjJiA=", "redirect_uri": "a", "code": "authcode",
			"code_verifier": "N1Zo9-8Rfwhkt68r1r29ty8YwIraXR8eh_1Qwxg7yQXsonBt",
		}), http.StatusOK)
		var tok struct {
			AccessToken  string `json:"access_token"`
			RefreshToken string `json:"refresh_token"`
		}
		require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &tok))
		MakeRequest(t, NewRequest(t, "GET", "/api/v1/user").AddTokenAuth(tok.AccessToken), http.StatusOK)

		sess := loginUser(t, "user1")
		doc := NewHTMLParser(t, sess.MakeRequest(t, NewRequest(t, "GET", "/user/settings/applications"), http.StatusOK).Body)
		revoke := doc.Find(`section[aria-labelledby="sp-grants"] button[data-url="/user/settings/applications/oauth2/1/revoke/1"]`)
		require.Equal(t, 1, revoke.Length())

		sess.MakeRequest(t, NewRequest(t, "POST", "/user/settings/applications/oauth2/1/revoke/1"), http.StatusOK)
		unittest.AssertNotExistsBean(t, &auth_model.OAuth2Grant{ID: 1})
		MakeRequest(t, NewRequest(t, "GET", "/api/v1/user").AddTokenAuth(tok.AccessToken), http.StatusUnauthorized)
		MakeRequest(t, NewRequestWithValues(t, "POST", "/login/oauth/access_token", map[string]string{
			"grant_type": "refresh_token", "client_id": "da7da3ba-9a13-4167-856f-3899de0b0138",
			"client_secret": "4MK8Na6R55smdCY0WuCCumZ6hjRPnGY5saWVRHHjJiA=", "refresh_token": tok.RefreshToken,
		}), http.StatusBadRequest)

		// the message after the page reloads
		doc = NewHTMLParser(t, sess.MakeRequest(t, NewRequest(t, "GET", "/user/settings/applications"), http.StatusOK).Body)
		assert.Contains(t, strings.TrimSpace(doc.Find(".flash-success").Text()), "revoked")
	})
}
