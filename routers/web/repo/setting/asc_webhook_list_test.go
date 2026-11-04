// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package setting

import (
	"net/http"
	"net/url"
	"testing"

	"forgejo.org/models/db"
	"forgejo.org/models/unittest"
	"forgejo.org/models/webhook"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/templates"
	"forgejo.org/modules/test"
	"forgejo.org/services/context"
	"forgejo.org/services/contexttest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A hook that signs its deliveries gets a new secret, shown once; a hook of a type without one has nothing to regenerate.
func TestWebhookRegenerateSecret(t *testing.T) {
	unittest.PrepareTestEnv(t)

	before := unittest.AssertExistsAndLoadBean(t, &webhook.Webhook{ID: 1})
	ctx, _ := contexttest.MockContext(t, "POST /user2/repo1/settings/hooks/1/secret")
	contexttest.LoadUser(t, ctx, 2)
	contexttest.LoadRepo(t, ctx, 1)
	ctx.Data["PageIsRepoSettings"] = true
	ctx.SetParams(":id", "1")
	WebhookRegenerateSecret(ctx)

	assert.Equal(t, http.StatusSeeOther, ctx.Resp.Status())
	assert.Equal(t, "/user2/repo1/settings/hooks/1", ctx.Resp.Header().Get("Location"))
	after := unittest.AssertExistsAndLoadBean(t, &webhook.Webhook{ID: 1})
	assert.NotEqual(t, before.Secret, after.Secret)
	assert.GreaterOrEqual(t, len(after.Secret), 43)
	assert.Equal(t, "asc_hooks.secret_regenerated", ctx.Flash.SuccessMsg)
	assert.Equal(t, before.URL, after.URL, "only the secret changes")

	ctx, _ = contexttest.MockContext(t, "POST /user2/repo1/settings/hooks/2/secret")
	contexttest.LoadUser(t, ctx, 2)
	contexttest.LoadRepo(t, ctx, 1)
	ctx.Data["PageIsRepoSettings"] = true
	ctx.SetParams(":id", "2")
	WebhookRegenerateSecret(ctx)
	assert.Equal(t, http.StatusNotFound, ctx.Resp.Status())
}

// The list shows each hook's address, its events and its last delivery, and Edit; Replay is on the hook's page.
func TestWebhookListRows(t *testing.T) {
	unittest.PrepareTestEnv(t)

	ctx, resp := contexttest.MockContext(t, "GET /user2/repo1/settings/hooks")
	ctx.Render = templates.HTMLRenderer()
	contexttest.LoadUser(t, ctx, 2)
	contexttest.LoadRepo(t, ctx, 1)
	WebhookList(ctx)
	require.Equal(t, http.StatusOK, ctx.Resp.Status())

	body := resp.Body.String()
	assert.Contains(t, body, `<ul class="wh-list">`)
	assert.Contains(t, body, `href="/user2/repo1/settings/hooks/1"><span class="ds-c-3">http://</span>www.example.com/url1</a>`)
	assert.Contains(t, body, "www.example.com/url2")
	assert.Contains(t, body, "asc_hooks.list_desc")
	assert.Contains(t, body, `<p class="rp-set-lead">asc_hooks.lead</p>`)
	// hook 1's last delivery succeeded; hook 2 never delivered
	assert.Contains(t, body, `href="/user2/repo1/settings/hooks/1" aria-label="edit: http://www.example.com/url1"`)
	assert.NotContains(t, body, "/replay/")
	assert.Contains(t, body, "asc_hooks.never")
}

// The edit form never carries the stored secret: posting it empty keeps the secret, posting a value replaces it.
func TestWebhookUpdateKeepsSecret(t *testing.T) {
	unittest.PrepareTestEnv(t)

	update := func(secret string) int {
		ctx, _ := contexttest.MockContext(t, "POST /user2/repo1/settings/hooks/1")
		contexttest.LoadUser(t, ctx, 2)
		contexttest.LoadRepo(t, ctx, 1)
		ctx.Data["PageIsRepoSettings"] = true
		ctx.SetParams(":id", "1")
		form := url.Values{"payload_url": {"https://www.example.com/url1"}, "http_method": {"POST"}, "content_type": {"1"}, "events": {"push_only"}, "active": {"on"}, "secret": {secret}}
		ctx.Req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		ctx.Req.Form, ctx.Req.PostForm = form, form
		WebhookUpdate(ctx)
		return ctx.Resp.Status()
	}

	require.Equal(t, http.StatusSeeOther, update("s3cr3t"))
	assert.Equal(t, "s3cr3t", unittest.AssertExistsAndLoadBean(t, &webhook.Webhook{ID: 1}).Secret)
	require.Equal(t, http.StatusSeeOther, update(""))
	assert.Equal(t, "s3cr3t", unittest.AssertExistsAndLoadBean(t, &webhook.Webhook{ID: 1}).Secret, "an empty field keeps the secret")
}

// The Settings tab of a hook with a secret shows that one is set, with Replace beside it, never the secret itself.
func TestWebhookEditHidesSecret(t *testing.T) {
	unittest.PrepareTestEnv(t)

	w := unittest.AssertExistsAndLoadBean(t, &webhook.Webhook{ID: 1})
	w.Secret = "s3cr3t"
	require.NoError(t, webhook.UpdateWebhookSecret(db.DefaultContext, w))

	ctx, resp := contexttest.MockContext(t, "GET /user2/repo1/settings/hooks/1")
	ctx.Render = templates.HTMLRenderer()
	contexttest.LoadUser(t, ctx, 2)
	contexttest.LoadRepo(t, ctx, 1)
	ctx.Data["PageIsRepoSettings"] = true
	ctx.SetParams(":id", "1")
	WebhookEdit(ctx)
	require.Equal(t, http.StatusOK, ctx.Resp.Status())

	body := resp.Body.String()
	assert.Contains(t, body, `placeholder="••••••••••••"`)
	assert.Contains(t, body, `data-wh-replace="secret"`)
	assert.NotContains(t, body, "s3cr3t")
}

// The row switch pauses a hook at once and keeps its settings; switching it on again resumes it. It goes back to the list.
func TestWebhookSetActive(t *testing.T) {
	unittest.PrepareTestEnv(t)

	set := func(active string) *webhook.Webhook {
		ctx, _ := contexttest.MockContext(t, "POST /user2/repo1/settings/hooks/1/active")
		contexttest.LoadUser(t, ctx, 2)
		contexttest.LoadRepo(t, ctx, 1)
		ctx.Data["PageIsRepoSettings"] = true
		ctx.SetParams(":id", "1")
		form := url.Values{"active": {active}, "redirect_to": {"/user2/repo1/settings/hooks"}}
		ctx.Req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		ctx.Req.Form, ctx.Req.PostForm = form, form
		WebhookSetActive(ctx)
		assert.Equal(t, http.StatusSeeOther, ctx.Resp.Status())
		assert.Equal(t, "/user2/repo1/settings/hooks", ctx.Resp.Header().Get("Location"))
		return unittest.AssertExistsAndLoadBean(t, &webhook.Webhook{ID: 1})
	}
	before := unittest.AssertExistsAndLoadBean(t, &webhook.Webhook{ID: 1})
	require.False(t, before.IsActive, "the fixture hook starts paused")

	resumed := set("true")
	assert.True(t, resumed.IsActive)
	assert.Equal(t, before.URL, resumed.URL)
	assert.Equal(t, before.Events, resumed.Events, "only the switch changes")
	assert.False(t, set("false").IsActive)
}

// A hook whose latest deliveries failed in a row says so in its row.
func TestWebhookListFailing(t *testing.T) {
	unittest.PrepareTestEnv(t)

	for range 3 {
		task, err := webhook.CreateHookTask(db.DefaultContext, &webhook.HookTask{HookID: 1, PayloadVersion: 2, PayloadContent: "{}"})
		require.NoError(t, err)
		task.IsDelivered = true
		task.IsSucceed = false
		task.ResponseInfo = &webhook.HookResponse{Status: http.StatusBadGateway}
		require.NoError(t, webhook.UpdateHookTask(db.DefaultContext, task))
	}

	ctx, resp := contexttest.MockContext(t, "GET /user2/repo1/settings/hooks")
	ctx.Render = templates.HTMLRenderer()
	contexttest.LoadUser(t, ctx, 2)
	contexttest.LoadRepo(t, ctx, 1)
	WebhookList(ctx)
	require.Equal(t, http.StatusOK, ctx.Resp.Status())

	body := resp.Body.String()
	assert.Contains(t, body, "asc_hooks.failing")
	assert.Contains(t, body, "asc_hooks.kind_generic")
	assert.Contains(t, body, `action="/user2/repo1/settings/hooks/1/active"`)
	assert.Contains(t, body, `role="switch" aria-checked="false"`)
}

// With asc on, the form refuses a plain http address under the field, keeps what was typed, and saves nothing.
func TestWebhookCreateRefusesHTTP(t *testing.T) {
	unittest.PrepareTestEnv(t)
	defer test.MockVariableValue(&setting.Asc.Enabled, true)()
	defer test.MockVariableValue(&setting.Webhook.AllowLocalHTTP, false)()

	create := func(payloadURL string) (int, string) {
		ctx, resp := contexttest.MockContext(t, "POST /user2/repo1/settings/hooks/forgejo/new")
		ctx.Render = templates.HTMLRenderer()
		contexttest.LoadUser(t, ctx, 2)
		contexttest.LoadRepo(t, ctx, 1)
		ctx.Data["PageIsRepoSettings"] = true
		ctx.SetParams(":type", "forgejo")
		form := url.Values{"payload_url": {payloadURL}, "http_method": {"POST"}, "content_type": {"1"}, "events": {"push_only"}, "active": {"on"}}
		ctx.Req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		ctx.Req.Form, ctx.Req.PostForm = form, form
		WebhookCreate(ctx)
		return ctx.Resp.Status(), resp.Body.String()
	}

	before := unittest.GetCount(t, &webhook.Webhook{RepoID: 1})
	status, body := create("http://receiver.example.org/hook")
	require.Equal(t, http.StatusUnprocessableEntity, status)
	assert.Contains(t, body, `role="alert"`)
	assert.Contains(t, body, "asc_hooks.http_refused")
	assert.Contains(t, body, `aria-invalid="true"`)
	assert.Contains(t, body, `value="http://receiver.example.org/hook"`, "the typed address is kept")
	assert.Equal(t, before, unittest.GetCount(t, &webhook.Webhook{RepoID: 1}))

	status, _ = create("https://receiver.example.org/hook")
	require.Equal(t, http.StatusSeeOther, status)
	assert.Equal(t, before+1, unittest.GetCount(t, &webhook.Webhook{RepoID: 1}))
}

// The edit form says up front that only https is accepted.
func TestWebhookFormHintsHTTPS(t *testing.T) {
	unittest.PrepareTestEnv(t)
	defer test.MockVariableValue(&setting.Asc.Enabled, true)()

	ctx, resp := contexttest.MockContext(t, "GET /user2/repo1/settings/hooks/1")
	ctx.Render = templates.HTMLRenderer()
	contexttest.LoadUser(t, ctx, 2)
	contexttest.LoadRepo(t, ctx, 1)
	ctx.Data["PageIsRepoSettings"] = true
	ctx.SetParams(":id", "1")
	WebhookEdit(ctx)
	require.Equal(t, http.StatusOK, ctx.Resp.Status())
	body := resp.Body.String()
	assert.Contains(t, body, `<p class="ds-hint" id="payload_url_help">asc_hooks.url_hint</p>`)
	assert.Contains(t, body, `aria-describedby="payload_url_help"`)
	assert.NotContains(t, body, "asc_hooks.http_refused")
}

// An existing hook opens on one card: its address, Delete, and the Settings and Recent deliveries tabs with the count.
func TestWebhookEditShowsHook(t *testing.T) {
	unittest.PrepareTestEnv(t)

	ctx, resp := contexttest.MockContext(t, "GET /user2/repo1/settings/hooks/1")
	ctx.Render = templates.HTMLRenderer()
	contexttest.LoadUser(t, ctx, 2)
	contexttest.LoadRepo(t, ctx, 1)
	ctx.Data["PageIsRepoSettings"] = true
	ctx.Data["Link"] = "/user2/repo1/settings/hooks/1"
	ctx.Data["Permission"] = &ctx.Repo.Permission
	context.UnitTypes()(ctx)
	ctx.SetParams(":id", "1")
	WebhookEdit(ctx)
	require.Equal(t, http.StatusOK, ctx.Resp.Status())

	body := resp.Body.String()
	assert.Contains(t, body, `id="wh-hook-url">http://www.example.com/url1</h2>`)
	assert.Contains(t, body, `data-modal-id="delete-webhook"`)
	assert.Contains(t, body, "asc_hooks.tab_settings")
	assert.Contains(t, body, `href="/user2/repo1/settings/hooks/1?tab=deliveries"`)
	assert.Contains(t, body, `id="payload_url"`, "the settings tab holds the form")
	assert.Contains(t, body, `data-redirect="/user2/repo1/settings/hooks/1?tab=deliveries" disabled`, "a paused hook sends no test delivery")
	assert.Contains(t, body, "asc_hooks.send_test")
	assert.NotContains(t, body, "wh-dl-grid")
	assert.NotContains(t, body, "asc_hooks.failing_chip", "one failure in the fixtures is not a streak")
}

// The deliveries tab lists a hook's deliveries beside the selected one: a failed one says why, with Redeliver, and its
// request and response are one link apart.
func TestWebhookDeliveries(t *testing.T) {
	unittest.PrepareTestEnv(t)

	var uuid string
	for range 2 {
		task, err := webhook.CreateHookTask(db.DefaultContext, &webhook.HookTask{HookID: 1, PayloadVersion: 2, EventType: "release", PayloadContent: `{"action":"published"}`})
		require.NoError(t, err)
		task.IsDelivered = true
		task.DurationMS = 1200
		task.RequestInfo = &webhook.HookRequest{URL: "https://hooks.example.org/in", HTTPMethod: "POST", Headers: map[string]string{"X-Asc-Event": "release"}, Body: `{"action":"published"}`}
		task.ResponseInfo = &webhook.HookResponse{Status: http.StatusBadGateway, Body: `{"error":"upstream down"}`}
		require.NoError(t, webhook.UpdateHookTask(db.DefaultContext, task))
		uuid = task.UUID
	}

	get := func(form url.Values) string {
		ctx, resp := contexttest.MockContext(t, "GET /user2/repo1/settings/hooks/1")
		ctx.Render = templates.HTMLRenderer()
		contexttest.LoadUser(t, ctx, 2)
		contexttest.LoadRepo(t, ctx, 1)
		ctx.Data["PageIsRepoSettings"] = true
		ctx.Link = "/user2/repo1/settings/hooks/1"
		ctx.Data["Link"] = ctx.Link
		ctx.Data["Permission"] = &ctx.Repo.Permission
		context.UnitTypes()(ctx)
		ctx.SetParams(":id", "1")
		ctx.Req.Form = form
		WebhookEdit(ctx)
		require.Equal(t, http.StatusOK, ctx.Resp.Status())
		return resp.Body.String()
	}

	body := get(url.Values{"tab": {"deliveries"}, "delivery": {uuid}})
	assert.Contains(t, body, "asc_hooks.failing_chip", "two failures in a row are a streak")
	assert.Contains(t, body, `class="wh-dl-li" href="/user2/repo1/settings/hooks/1?tab=deliveries&amp;page=1&delivery=`+uuid+`" aria-current="true"`)
	assert.Contains(t, body, "uuid3", "the fixture deliveries are listed too")
	assert.Contains(t, body, "asc_hooks.response_after_s")
	assert.Contains(t, body, `action="/user2/repo1/settings/hooks/1/replay/`+uuid+`"`)
	assert.Contains(t, body, "asc_hooks.redeliver")
	assert.Contains(t, body, `role="alert"`)
	assert.Contains(t, body, "asc_hooks.failed_banner")
	assert.Contains(t, body, `delivery=`+uuid+`&part=response"`)
	assert.Contains(t, body, "published")
	assert.NotContains(t, body, "upstream down", "the request is shown first")

	body = get(url.Values{"tab": {"deliveries"}, "delivery": {uuid}, "part": {"response"}})
	assert.Contains(t, body, "upstream down")

	body = get(url.Values{"tab": {"deliveries"}, "delivery": {"uuid3"}})
	assert.NotContains(t, body, `role="alert"`, "a delivery that succeeded has no error")
}
