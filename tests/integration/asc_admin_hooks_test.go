// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package integration

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"forgejo.org/models/db"
	"forgejo.org/models/unittest"
	"forgejo.org/models/webhook"
	webhook_module "forgejo.org/modules/webhook"
	app_context "forgejo.org/services/context"
	"forgejo.org/tests"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// adminHooksFlash returns the success message a response sets in the flash cookie.
func adminHooksFlash(t *testing.T, resp interface{ Result() *http.Response }) string {
	t.Helper()
	for _, c := range resp.Result().Cookies() {
		if c.Name != app_context.CookieNameFlash {
			continue
		}
		v, err := url.ParseQuery(c.Value)
		require.NoError(t, err)
		if m := v.Get("success"); m != "" {
			return m
		}
		// the flash can arrive encoded twice
		for k := range v {
			if inner, err := url.ParseQuery(k); err == nil && inner.Get("success") != "" {
				return inner.Get("success")
			}
		}
	}
	return ""
}

// The board notes of AdminHooks: the empty state, a failing delivery with Replay, which re-sends the last failed
// delivery and comes back to the list, and a regenerated secret that is shown once in the message and never on a page.
func TestAscAdminHooksBoardNotes(t *testing.T) {
	defer tests.PrepareTestEnv(t)()
	session := loginUser(t, "user1")

	t.Run("empty: no system or default hooks", func(t *testing.T) {
		resp := session.MakeRequest(t, NewRequest(t, "GET", "/admin/hooks"), http.StatusOK)
		doc := NewHTMLParser(t, resp.Body)
		hooks := doc.doc.Find(`section[aria-labelledby="adm-hooks"]`)
		assert.Contains(t, hooks.Find(".ds-empty").Text(), "No webhooks")
		assert.Equal(t, 0, hooks.Find(".wh-table").Length())
	})

	w := &webhook.Webhook{URL: "https://logs.acme.example/in", IsSystemWebhook: true, IsActive: true, Type: webhook_module.FORGEJO, Secret: "first-secret"}
	w.HookEvent = &webhook_module.HookEvent{SendEverything: true}
	w.ContentType = webhook.ContentTypeJSON
	require.NoError(t, w.UpdateEvent())
	require.NoError(t, db.Insert(db.DefaultContext, w))
	var last *webhook.HookTask
	for range 2 {
		task, err := webhook.CreateHookTask(db.DefaultContext, &webhook.HookTask{HookID: w.ID, PayloadVersion: 2, PayloadContent: "{}"})
		require.NoError(t, err)
		task.IsDelivered = true
		task.IsSucceed = false
		task.ResponseInfo = &webhook.HookResponse{Status: http.StatusBadGateway}
		require.NoError(t, webhook.UpdateHookTask(db.DefaultContext, task))
		last = task
	}

	t.Run("failing delivery: Replay re-sends the last failed delivery", func(t *testing.T) {
		resp := session.MakeRequest(t, NewRequest(t, "GET", "/admin/hooks"), http.StatusOK)
		doc := NewHTMLParser(t, resp.Body)
		row := doc.doc.Find(`section[aria-labelledby="adm-hooks"] .wh-table tbody tr`)
		require.Equal(t, 1, row.Length())
		assert.Contains(t, row.Find(".wh-failing").Text(), "Failing: the last 2 deliveries")
		action := fmt.Sprintf("/admin/hooks/%d/replay/%s", w.ID, last.UUID)
		form := row.Find(`form[action="` + action + `"]`)
		require.Equal(t, 1, form.Length(), "Replay sends the latest failed delivery")
		assert.Equal(t, "/admin/hooks", form.Find(`input[name="redirect_to"]`).AttrOr("value", ""))
		assert.Equal(t, 0, row.Find(`a[aria-label^="Edit"]`).Length(), "a failed row offers Replay, not Edit")

		before := unittest.GetCount(t, &webhook.HookTask{HookID: w.ID})
		resp = session.MakeRequest(t, NewRequestWithValues(t, "POST", action, map[string]string{"redirect_to": "/admin/hooks"}), http.StatusSeeOther)
		assert.Equal(t, "/admin/hooks", resp.Header().Get("Location"), "back to the list")
		assert.NotEmpty(t, adminHooksFlash(t, resp))
		assert.Equal(t, before+1, unittest.GetCount(t, &webhook.HookTask{HookID: w.ID}), "one delivery is queued again")
	})

	t.Run("secret regenerated: shown once in the message, never on the page", func(t *testing.T) {
		resp := session.MakeRequest(t, NewRequest(t, "GET", fmt.Sprintf("/admin/hooks/%d", w.ID)), http.StatusOK)
		assert.NotContains(t, resp.Body.String(), "first-secret")

		resp = session.MakeRequest(t, NewRequest(t, "POST", fmt.Sprintf("/admin/hooks/%d/secret", w.ID)), http.StatusSeeOther)
		assert.Equal(t, fmt.Sprintf("/admin/hooks/%d", w.ID), resp.Header().Get("Location"))
		after := unittest.AssertExistsAndLoadBean(t, &webhook.Webhook{ID: w.ID})
		require.NotEqual(t, "first-secret", after.Secret)
		msg := adminHooksFlash(t, resp)
		assert.True(t, strings.HasPrefix(msg, "Secret regenerated: "+after.Secret), msg)

		// The page the message lands on shows it once; the page after that shows only that a secret is set.
		resp = session.MakeRequest(t, NewRequest(t, "GET", fmt.Sprintf("/admin/hooks/%d", w.ID)), http.StatusOK)
		assert.Equal(t, 1, strings.Count(resp.Body.String(), after.Secret), "once, in the message")
		assert.NotContains(t, NewHTMLParser(t, resp.Body).doc.Find(`input[name="secret"]`).AttrOr("value", ""), after.Secret)
		resp = session.MakeRequest(t, NewRequest(t, "GET", fmt.Sprintf("/admin/hooks/%d", w.ID)), http.StatusOK)
		assert.NotContains(t, resp.Body.String(), after.Secret, "never again")
		assert.Contains(t, resp.Body.String(), `data-wh-replace="secret"`)
	})
}
