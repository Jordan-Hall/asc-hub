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
	"forgejo.org/modules/templates"
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

// The list shows each hook's host, its events and its last delivery, with Replay only where that delivery failed.
func TestWebhookListRows(t *testing.T) {
	unittest.PrepareTestEnv(t)

	ctx, resp := contexttest.MockContext(t, "GET /user2/repo1/settings/hooks")
	ctx.Render = templates.HTMLRenderer()
	contexttest.LoadUser(t, ctx, 2)
	contexttest.LoadRepo(t, ctx, 1)
	WebhookList(ctx)
	require.Equal(t, http.StatusOK, ctx.Resp.Status())

	body := resp.Body.String()
	assert.Contains(t, body, `class="ds-table ds-table-stack wh-table"`)
	assert.Contains(t, body, `href="/user2/repo1/settings/hooks/1">www.example.com</a>`)
	assert.Contains(t, body, "http://www.example.com/url2")
	// hook 1's last delivery succeeded: Edit, no Replay; hook 2 never delivered
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

// The edit form of a hook with a secret shows that one is set, never the secret itself.
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
	assert.Contains(t, body, `placeholder="asc_hooks.secret_set"`)
	assert.NotContains(t, body, "s3cr3t")
}
