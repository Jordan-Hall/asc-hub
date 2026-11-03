// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package admin

import (
	"net/http"
	"strings"
	"testing"

	"forgejo.org/models/db"
	"forgejo.org/models/unittest"
	"forgejo.org/models/webhook"
	"forgejo.org/modules/templates"
	webhook_module "forgejo.org/modules/webhook"
	"forgejo.org/services/contexttest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// System and default webhooks are one list; each row says which it is.
func TestAdminHooksOneList(t *testing.T) {
	unittest.PrepareTestEnv(t)
	for _, w := range []*webhook.Webhook{
		{URL: "https://logs.acme.example/in", IsSystemWebhook: true, Type: webhook_module.FORGEJO},
		{URL: "https://deploy.acme.example/asc", Type: webhook_module.FORGEJO},
	} {
		w.HookEvent = &webhook_module.HookEvent{SendEverything: true}
		w.ContentType = webhook.ContentTypeJSON
		require.NoError(t, w.UpdateEvent())
		require.NoError(t, db.Insert(db.DefaultContext, w))
	}

	ctx, resp := contexttest.MockContext(t, "GET /admin/hooks")
	ctx.Render = templates.HTMLRenderer()
	contexttest.LoadUser(t, ctx, 1)
	DefaultOrSystemWebhooks(ctx)
	require.Equal(t, http.StatusOK, ctx.Resp.Status())

	body := resp.Body.String()
	assert.Equal(t, 1, strings.Count(body, `>asc_hooks.col_webhook</th>`), "one list of webhooks")
	assert.Contains(t, body, "asc_hooks.col_scope")
	sys := strings.Index(body, ">logs.acme.example</a>")
	def := strings.Index(body, ">deploy.acme.example</a>")
	require.Positive(t, sys)
	require.Greater(t, def, sys, "system hooks come first")
	assert.Contains(t, body[sys:def], "asc_admin.hooks_scope_system")
	assert.Contains(t, body[def:], "asc_admin.hooks_scope_default")
	assert.Contains(t, body, `data-modal="#adm-add-hook-0"`)
	assert.Contains(t, body, `data-modal="#adm-add-hook-1"`)
	assert.Contains(t, body, `href="/admin/system-hooks/forgejo/new"`)
	assert.Contains(t, body, `href="/admin/default-hooks/forgejo/new"`)

	// every OAuth application on the site, people's too, with its owner and its grants
	assert.Contains(t, body, "asc_admin.apps_all_desc")
	app1 := strings.Index(body, "<b>Test</b>")
	require.Positive(t, app1)
	row := body[app1:]
	row = row[:strings.Index(row, "</tr>")]
	assert.Contains(t, row, `href="/user1"`)
	assert.Contains(t, row, `>3</td>`)
	assert.Contains(t, row, `href="/admin/applications/oauth2/1"`)
}

// An admin manages a person's OAuth application as well as the site's.
func TestAdminManagesPeoplesApps(t *testing.T) {
	unittest.PrepareTestEnv(t)

	ctx, _ := contexttest.MockContext(t, "GET /admin/applications/oauth2/2")
	ctx.Render = templates.HTMLRenderer()
	contexttest.LoadUser(t, ctx, 1)
	ctx.SetParams("id", "2")
	EditApplication(ctx)
	assert.Equal(t, http.StatusOK, ctx.Resp.Status())

	ctx, _ = contexttest.MockContext(t, "GET /admin/applications/oauth2/999")
	ctx.Render = templates.HTMLRenderer()
	contexttest.LoadUser(t, ctx, 1)
	ctx.SetParams("id", "999")
	EditApplication(ctx)
	assert.Equal(t, http.StatusNotFound, ctx.Resp.Status())
}
