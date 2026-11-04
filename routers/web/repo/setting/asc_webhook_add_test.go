// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package setting

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"testing"

	"forgejo.org/models/db"
	"forgejo.org/models/unittest"
	"forgejo.org/models/webhook"
	"forgejo.org/modules/gitrepo"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/test"
	webhook_module "forgejo.org/modules/webhook"
	"forgejo.org/services/contexttest"
	webhook_service "forgejo.org/services/webhook"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// addHook posts the Add webhook dialog of user2/repo1 as its script does and returns the status and the answer.
func addHook(t *testing.T, form url.Values) (int, map[string]string) {
	t.Helper()
	ctx, resp := contexttest.MockContext(t, "POST /user2/repo1/settings/hooks/add")
	contexttest.LoadUser(t, ctx, 2)
	contexttest.LoadRepo(t, ctx, 1)
	ctx.Data["PageIsRepoSettings"] = true
	ctx.Req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	ctx.Req.Header.Set("Accept", "application/json")
	ctx.Req.Form, ctx.Req.PostForm = form, form
	WebhookAdd(ctx)
	var got map[string]string
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &got), resp.Body.String())
	return ctx.Resp.Status(), got
}

// lastHook is the newest webhook of repo1.
func lastHook(t *testing.T) *webhook.Webhook {
	t.Helper()
	ws, err := db.Find[webhook.Webhook](db.DefaultContext, webhook.ListWebhookOptions{RepoID: 1})
	require.NoError(t, err)
	last := ws[0]
	for _, w := range ws {
		if w.ID > last.ID {
			last = w
		}
	}
	return last
}

// A generic hook gets JSON, POST, a new secret and change.landed; a chat hook takes the format of its host.
func TestWebhookAdd(t *testing.T) {
	unittest.PrepareTestEnv(t)

	status, got := addHook(t, url.Values{"kind": {"generic"}, "payload_url": {" https://ci.example.org/hook "}})
	require.Equal(t, http.StatusOK, status, got)
	w := lastHook(t)
	assert.Equal(t, "/user2/repo1/settings/hooks/"+strconv.FormatInt(w.ID, 10), got["redirect"])
	assert.Equal(t, webhook_module.FORGEJO, w.Type)
	assert.Equal(t, "https://ci.example.org/hook", w.URL)
	assert.Equal(t, http.MethodPost, w.HTTPMethod)
	assert.Equal(t, webhook.ContentTypeJSON, w.ContentType)
	assert.Len(t, w.Secret, len(webhook_service.NewSecret()))
	assert.True(t, w.IsActive)
	assert.True(t, w.HasAscEvent(webhook_module.HookEventChangeLanded))
	assert.False(t, w.HasPushEvent(), "a new hook sends only what it was added for")

	for addr, want := range map[string]webhook_module.HookType{
		"https://discord.com/api/webhooks/1/x": webhook_module.DISCORD,
		"https://chat.example.org/hooks/x":     webhook_module.SLACK,
	} {
		status, got = addHook(t, url.Values{"kind": {"chat"}, "payload_url": {addr}})
		require.Equal(t, http.StatusOK, status, got)
		w = lastHook(t)
		assert.Equal(t, want, w.Type, addr)
		assert.Empty(t, w.Secret, addr)
		assert.True(t, w.HasAscEvent(webhook_module.HookEventChangeLanded), addr)
	}
}

// A refused address, an unknown kind and an agent hook where asc does not serve the repository answer 422 with the
// field, and nothing is added.
func TestWebhookAddRefused(t *testing.T) {
	unittest.PrepareTestEnv(t)
	defer test.MockVariableValue(&setting.Asc.Enabled, true)()
	before := unittest.GetCount(t, &webhook.Webhook{})

	for name, c := range map[string]struct {
		form  url.Values
		field string
	}{
		"not an address": {url.Values{"kind": {"generic"}, "payload_url": {"ci.example.org"}}, "payload_url"},
		"plain http":     {url.Values{"kind": {"chat"}, "payload_url": {"http://ci.example.org/hook"}}, "payload_url"},
		"no kind":        {url.Values{"payload_url": {"https://ci.example.org/hook"}}, "kind"},
		"no asc":         {url.Values{"kind": {"agent"}, "agent": {"fixer"}, "task": {"Fix it."}}, "kind"},
	} {
		status, got := addHook(t, c.form)
		require.Equal(t, http.StatusUnprocessableEntity, status, name)
		assert.Equal(t, c.field, got["field"], name)
		assert.NotEmpty(t, got["error"], name)
	}
	assert.Equal(t, before, unittest.GetCount(t, &webhook.Webhook{}))
}

// In a repository that asc serves, an agent hook runs its agent with its task on the default branch, as the person
// who added it, on change.landed; a wrong agent or task answers 422 with that field.
func TestWebhookAddAgent(t *testing.T) {
	unittest.PrepareTestEnv(t)
	defer test.MockVariableValue(&setting.Asc.Enabled, true)()
	_, err := db.GetEngine(db.DefaultContext).Exec("UPDATE repository SET storage_backend = ? WHERE id = 1", string(gitrepo.StorageBackendAsc))
	require.NoError(t, err)

	status, got := addHook(t, url.Values{"kind": {"agent"}, "agent": {" fixer "}, "task": {"Fix the build."}, "payload_url": {"https://ignored.example.org"}})
	require.Equal(t, http.StatusOK, status, got)
	w := lastHook(t)
	assert.Equal(t, webhook_module.ASCAGENT, w.Type)
	assert.Equal(t, "asc-agent://fixer", w.URL)
	assert.Empty(t, w.Secret)
	assert.Equal(t, webhook_service.AgentMeta{Agent: "fixer", Target: "master", Task: "Fix the build.", RunAs: 2}, *webhook_service.GetAgentMeta(w))
	assert.True(t, w.HasAscEvent(webhook_module.HookEventChangeLanded))

	before := unittest.GetCount(t, &webhook.Webhook{})
	for field, form := range map[string]url.Values{
		"agent": {"kind": {"agent"}, "agent": {"Fixer One"}, "task": {"Fix it."}},
		"task":  {"kind": {"agent"}, "agent": {"fixer"}, "task": {"  "}},
	} {
		status, got := addHook(t, form)
		require.Equal(t, http.StatusUnprocessableEntity, status, field)
		assert.Equal(t, field, got["field"])
	}
	assert.Equal(t, before, unittest.GetCount(t, &webhook.Webhook{}))
}
