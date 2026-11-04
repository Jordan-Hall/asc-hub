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
	ctx.Req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if asJSON {
		ctx.Req.Header.Set("Accept", "application/json")
	}
	ctx.Req.Form, ctx.Req.PostForm = form, form
	WebhookSaveSettings(ctx)
	return ctx.Resp.Status(), resp.Body.String(), unittest.AssertExistsAndLoadBean(t, &webhook.Webhook{ID: 1})
}

// A save from the page's script stores the address, content type, secret and ASC events, keeps the hook's other
// settings and answers {"ok":true}; an empty secret keeps the stored one.
func TestWebhookSaveSettings(t *testing.T) {
	unittest.PrepareTestEnv(t)
	before := unittest.AssertExistsAndLoadBean(t, &webhook.Webhook{ID: 1})

	status, body, w := saveSettings(t, url.Values{
		"payload_url":  {" https://ci.example.org/hook "},
		"content_type": {"2"},
		"secret":       {"s3cr3t"},
		"asc_events":   {"change.landed", "agent_run.finished"},
	}, true)
	require.Equal(t, http.StatusOK, status)
	assert.JSONEq(t, `{"ok":true}`, body)
	assert.Equal(t, "https://ci.example.org/hook", w.URL)
	assert.Equal(t, webhook.ContentTypeForm, w.ContentType)
	assert.Equal(t, "s3cr3t", w.Secret)
	assert.True(t, w.HasAscEvent(webhook_module.HookEventChangeLanded))
	assert.True(t, w.HasAscEvent(webhook_module.HookEventAgentRunFinished))
	assert.False(t, w.HasAscEvent(webhook_module.HookEventChangeOpened))
	assert.Equal(t, before.HTTPMethod, w.HTTPMethod)
	assert.Equal(t, before.IsActive, w.IsActive)

	status, _, w = saveSettings(t, url.Values{"payload_url": {"https://ci.example.org/hook"}, "content_type": {"1"}, "secret": {""}}, true)
	require.Equal(t, http.StatusOK, status)
	assert.Equal(t, "s3cr3t", w.Secret, "an empty field keeps the secret")
	assert.Equal(t, webhook.ContentTypeJSON, w.ContentType)
	assert.False(t, w.HasAscEvent(webhook_module.HookEventChangeLanded), "an unticked event is no longer sent")
}

// A refused address answers 422 with the field and its message and changes nothing; a save without script goes back
// to the hook.
func TestWebhookSaveSettingsRefused(t *testing.T) {
	unittest.PrepareTestEnv(t)
	defer test.MockVariableValue(&setting.Asc.Enabled, true)()
	before := unittest.AssertExistsAndLoadBean(t, &webhook.Webhook{ID: 1})

	for _, bad := range []string{"not a url", "http://ci.example.org/hook"} {
		status, body, w := saveSettings(t, url.Values{"payload_url": {bad}}, true)
		require.Equal(t, http.StatusUnprocessableEntity, status, bad)
		var problem map[string]string
		require.NoError(t, json.Unmarshal([]byte(body), &problem))
		assert.Equal(t, "payload_url", problem["field"], bad)
		assert.NotEmpty(t, problem["error"], bad)
		assert.Equal(t, before.URL, w.URL, bad)
	}

	status, _, w := saveSettings(t, url.Values{"payload_url": {"https://ci.example.org/hook"}}, false)
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
