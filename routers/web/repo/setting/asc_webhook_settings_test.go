// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package setting

import (
	"encoding/json"
	"net/http"
	"net/url"
	"testing"

	"forgejo.org/models/unittest"
	"forgejo.org/models/webhook"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/test"
	webhook_module "forgejo.org/modules/webhook"
	app_context "forgejo.org/services/context"
	"forgejo.org/services/contexttest"
	webhook_service "forgejo.org/services/webhook"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// saveSettings posts the Settings tab of hook 1 of user2/repo1, asking for JSON when asJSON is set, and returns the
// status, the body and the hook as stored afterwards.
func saveSettings(t *testing.T, form url.Values, asJSON bool) (int, string, *webhook.Webhook) {
	t.Helper()
	ctx, resp := contexttest.MockContext(t, "POST /user2/repo1/settings/hooks/1/settings")
	contexttest.LoadUser(t, ctx, 2)
	contexttest.LoadRepo(t, ctx, 1)
	ctx.Data["PageIsRepoSettings"] = true
	ctx.SetParams(":id", "1")
	ctx.AppendContextValue(app_context.WebContextKey, ctx) // as the server does: the form checks of some kinds read it
	ctx.Req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if asJSON {
		ctx.Req.Header.Set("Accept", "application/json")
	}
	ctx.Req.Form, ctx.Req.PostForm = form, form
	WebhookSaveSettings(ctx)
	return ctx.Resp.Status(), resp.Body.String(), unittest.AssertExistsAndLoadBean(t, &webhook.Webhook{ID: 1})
}

// sendForm is the Settings tab of a generic hook as the page sends it, with the fields in more changed.
func sendForm(more url.Values) url.Values {
	form := url.Values{"payload_url": {"https://ci.example.org/hook"}, "http_method": {"POST"}, "content_type": {"1"}, "branch_filter": {"*"}}
	for k, v := range more {
		form[k] = v
	}
	return form
}

// A save from the page's script stores the address, method, content type, secret, branch filter, Authorization
// header and ASC events, keeps the Active switch and answers {"ok":true}; an empty secret keeps the stored one and an
// empty header removes it.
func TestWebhookSaveSettings(t *testing.T) {
	unittest.PrepareTestEnv(t)
	before := unittest.AssertExistsAndLoadBean(t, &webhook.Webhook{ID: 1})

	status, body, w := saveSettings(t, sendForm(url.Values{
		"payload_url":          {" https://ci.example.org/hook "},
		"http_method":          {"GET"},
		"content_type":         {"2"},
		"secret":               {"s3cr3t"},
		"branch_filter":        {"{main,release/*}"},
		"authorization_header": {"Bearer token123"},
		"asc_events":           {"change.landed", "agent_run.finished"},
	}), true)
	require.Equal(t, http.StatusOK, status, body)
	assert.JSONEq(t, `{"ok":true}`, body)
	assert.Equal(t, "https://ci.example.org/hook", w.URL)
	assert.Equal(t, http.MethodGet, w.HTTPMethod)
	assert.Equal(t, webhook.ContentTypeForm, w.ContentType)
	assert.Equal(t, "s3cr3t", w.Secret)
	assert.Equal(t, "{main,release/*}", w.BranchFilter)
	header, err := w.HeaderAuthorization()
	require.NoError(t, err)
	assert.Equal(t, "Bearer token123", header)
	assert.True(t, w.HasAscEvent(webhook_module.HookEventChangeLanded))
	assert.True(t, w.HasAscEvent(webhook_module.HookEventAgentRunFinished))
	assert.False(t, w.HasAscEvent(webhook_module.HookEventChangeOpened))
	assert.Equal(t, before.IsActive, w.IsActive)

	status, _, w = saveSettings(t, sendForm(url.Values{"secret": {""}}), true)
	require.Equal(t, http.StatusOK, status)
	assert.Equal(t, "s3cr3t", w.Secret, "an empty field keeps the secret")
	assert.Equal(t, webhook.ContentTypeJSON, w.ContentType)
	assert.Equal(t, http.MethodPost, w.HTTPMethod)
	assert.Empty(t, w.HeaderAuthorizationEncrypted, "an empty field removes the header")
	assert.False(t, w.HasAscEvent(webhook_module.HookEventChangeLanded), "an unticked event is no longer sent")
}

// A Matrix hook saves its homeserver, room, message type and token and works out its address; a Slack hook with a
// channel that is not one answers 422 with that field.
func TestWebhookSaveKindSettings(t *testing.T) {
	unittest.PrepareTestEnv(t)
	defer test.MockVariableValue(&setting.Asc.Enabled, true)()
	w := unittest.AssertExistsAndLoadBean(t, &webhook.Webhook{ID: 1})
	w.Type = webhook_module.MATRIX
	w.URL = "https://matrix.example.org/_matrix/client/v3/rooms/%21old/send/m.room.message"
	require.NoError(t, webhook.UpdateWebhook(t.Context(), w))

	status, body, w := saveSettings(t, url.Values{
		"homeserver_url": {"https://matrix.example.org"},
		"room_id":        {"!room:example.org"},
		"message_type":   {"2"},
		"access_token":   {"tok"},
		"branch_filter":  {"main"},
	}, true)
	require.Equal(t, http.StatusOK, status, body)
	assert.Equal(t, "https://matrix.example.org/_matrix/client/v3/rooms/%21room:example.org/send/m.room.message", w.URL)
	assert.Equal(t, http.MethodPut, w.HTTPMethod)
	assert.Equal(t, "main", w.BranchFilter)
	assert.JSONEq(t, `{"homeserver_url":"https://matrix.example.org","room_id":"!room:example.org","message_type":2}`, w.Meta)
	header, err := w.HeaderAuthorization()
	require.NoError(t, err)
	assert.Equal(t, "Bearer tok", header)

	status, body, _ = saveSettings(t, url.Values{"homeserver_url": {"http://matrix.example.org"}, "room_id": {"!r"}, "access_token": {"tok"}}, true)
	require.Equal(t, http.StatusUnprocessableEntity, status, body)
	assert.Contains(t, body, `"field":"homeserver_url"`)

	w.Type = webhook_module.SLACK
	require.NoError(t, webhook.UpdateWebhook(t.Context(), w))
	status, body, after := saveSettings(t, url.Values{"payload_url": {"https://chat.example.org/hook"}, "channel": {"Not A Channel"}}, true)
	require.Equal(t, http.StatusUnprocessableEntity, status, body)
	var problem map[string]string
	require.NoError(t, json.Unmarshal([]byte(body), &problem))
	assert.Equal(t, "channel", problem["field"])
	assert.NotEmpty(t, problem["error"])
	assert.Equal(t, w.URL, after.URL, "nothing is saved")
}

// A refused address or branch filter answers 422 with the field and its message and changes nothing; a save without
// script goes back to the hook.
func TestWebhookSaveSettingsRefused(t *testing.T) {
	unittest.PrepareTestEnv(t)
	defer test.MockVariableValue(&setting.Asc.Enabled, true)()
	before := unittest.AssertExistsAndLoadBean(t, &webhook.Webhook{ID: 1})

	for _, c := range []struct {
		field string
		form  url.Values
	}{
		{"payload_url", url.Values{"payload_url": {"not a url"}}},
		{"payload_url", url.Values{"payload_url": {"http://ci.example.org/hook"}}},
		{"branch_filter", url.Values{"branch_filter": {"[main"}}},
	} {
		status, body, w := saveSettings(t, sendForm(c.form), true)
		require.Equal(t, http.StatusUnprocessableEntity, status, c.form)
		var problem map[string]string
		require.NoError(t, json.Unmarshal([]byte(body), &problem))
		assert.Equal(t, c.field, problem["field"], c.form)
		assert.NotEmpty(t, problem["error"], c.form)
		assert.Equal(t, before.URL, w.URL, c.form)
	}

	status, _, w := saveSettings(t, sendForm(nil), false)
	require.Equal(t, http.StatusSeeOther, status)
	assert.Equal(t, "https://ci.example.org/hook", w.URL)
}

// The Settings tab of an ASC agent hook saves its agent, branch and task, makes the person who saved them the one
// the runs are for, and keeps the agent run events off; a wrong field answers 422 with that field.
func TestWebhookSaveAgentSettings(t *testing.T) {
	unittest.PrepareTestEnv(t)
	defer test.MockVariableValue(&setting.Asc.Enabled, true)()
	w := unittest.AssertExistsAndLoadBean(t, &webhook.Webhook{ID: 1})
	w.Type = webhook_module.ASCAGENT
	w.URL = webhook_service.AgentURL("fixer")
	w.Meta = `{"agent":"fixer","target":"main","task":"Fix it.","run_as":1}`
	require.NoError(t, webhook.UpdateWebhook(t.Context(), w))

	status, body, w := saveSettings(t, url.Values{
		"agent":      {" reviewer "},
		"target":     {"release"},
		"task":       {"Review the change that landed."},
		"asc_events": {"change.landed", "agent_run.finished"},
	}, true)
	require.Equal(t, http.StatusOK, status, body)
	assert.Equal(t, "asc-agent://reviewer", w.URL)
	assert.Equal(t, webhook_service.AgentMeta{Agent: "reviewer", Target: "release", Task: "Review the change that landed.", RunAs: 2}, *webhook_service.GetAgentMeta(w))
	assert.True(t, w.HasAscEvent(webhook_module.HookEventChangeLanded))
	assert.False(t, w.HasAscEvent(webhook_module.HookEventAgentRunFinished), "an agent hook does not take agent run events")

	for field, form := range map[string]url.Values{
		"agent":  {"agent": {"Reviewer One"}, "target": {"main"}, "task": {"x"}},
		"target": {"agent": {"reviewer"}, "target": {""}, "task": {"x"}},
		"task":   {"agent": {"reviewer"}, "target": {"main"}, "task": {" "}},
	} {
		status, body, after := saveSettings(t, form, true)
		require.Equal(t, http.StatusUnprocessableEntity, status, field)
		var got map[string]string
		require.NoError(t, json.Unmarshal([]byte(body), &got))
		assert.Equal(t, field, got["field"])
		assert.NotEmpty(t, got["error"])
		assert.Equal(t, w.Meta, after.Meta, "nothing is saved")
	}
}
