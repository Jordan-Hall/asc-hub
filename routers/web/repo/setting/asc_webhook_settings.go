// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package setting

import (
	"fmt"
	"net/http"
	"strings"

	"forgejo.org/models/webhook"
	"forgejo.org/modules/json"
	"forgejo.org/modules/validation"
	webhook_module "forgejo.org/modules/webhook"
	"forgejo.org/services/context"
	webhook_service "forgejo.org/services/webhook"
)

// WebhookSaveSettings serves `POST {hooks}/{id}/settings`, the Settings tab of one hook, which saves each field when it
// changes: payload_url, for a hook that signs its deliveries content_type (1 json, 2 form) and secret (empty keeps
// the stored one), for an ASC agent hook agent, target and task instead, and asc_events, the ASC events it sends. Every other setting of the hook is kept. The page's script
// asks for JSON and gets {"ok":true}, or 422 with the field and the message to show under it; a form sent without
// script goes back to the hook with the message.
func WebhookSaveSettings(ctx *context.Context) {
	orCtx, w := checkWebhook(ctx)
	if ctx.Written() {
		return
	}
	back := fmt.Sprintf("%s/%d", orCtx.Link, w.ID)
	refuse := func(field, key string) {
		if wantsJSON(ctx) {
			ctx.JSON(http.StatusUnprocessableEntity, map[string]string{"field": field, "error": ctx.Locale.TrString(key)})
			return
		}
		ctx.Flash.Error(ctx.Tr(key))
		ctx.Redirect(back)
	}

	if w.Type == webhook_module.ASCAGENT {
		saveAgentSettings(ctx, w, refuse)
		if ctx.Written() {
			return
		}
	} else if !saveSendSettings(ctx, w, refuse) {
		return
	}
	w.ChooseAscEvents(ctx.Req.Form["asc_events"])
	if err := w.UpdateEvent(); err != nil {
		ctx.ServerError("UpdateEvent", err)
		return
	}
	if err := webhook.UpdateWebhook(ctx, w); err != nil {
		ctx.ServerError("UpdateWebhook", err)
		return
	}
	if wantsJSON(ctx) {
		ctx.JSON(http.StatusOK, map[string]bool{"ok": true})
		return
	}
	ctx.Flash.Success(ctx.Tr("repo.settings.update_hook_success"))
	ctx.Redirect(back)
}

// saveSendSettings reads the fields of a hook that sends requests into w: its address and, when it signs its
// deliveries, content type and secret. It returns false when it refused one.
func saveSendSettings(ctx *context.Context, w *webhook.Webhook, refuse func(field, key string)) bool {
	url := strings.TrimSpace(ctx.FormString("payload_url"))
	if !validation.IsValidURL(url) {
		refuse("payload_url", "asc_hooks.url_invalid")
		return false
	}
	if webhook_service.CheckPayloadURL(url) != nil {
		refuse("payload_url", "asc_hooks.http_refused")
		return false
	}
	w.URL = url
	if webhook_service.HasSecret(w.Type) {
		switch ctx.FormString("content_type") {
		case "1":
			w.ContentType = webhook.ContentTypeJSON
		case "2":
			w.ContentType = webhook.ContentTypeForm
		}
		if secret := ctx.FormString("secret"); secret != "" {
			w.Secret = secret
		}
	}
	return true
}

// saveAgentSettings reads the fields of an ASC agent hook into w: the agent, the branch and the task. The person
// who saves them is the one the runs are made for from then on, so the runs never do more than that person may.
// It writes the refusal when it refused one.
func saveAgentSettings(ctx *context.Context, w *webhook.Webhook, refuse func(field, key string)) {
	m, field := webhook_service.CheckAgentMeta(webhook_service.AgentMeta{
		Agent:  ctx.FormString("agent"),
		Target: ctx.FormString("target"),
		Task:   ctx.FormString("task"),
		RunAs:  ctx.Doer.ID,
	})
	if field != "" {
		refuse(field, "asc_hooks.agent_"+field+"_invalid")
		return
	}
	meta, err := json.Marshal(m)
	if err != nil {
		ctx.ServerError("Marshal", err)
		return
	}
	w.Meta = string(meta)
	w.URL = webhook_service.AgentURL(m.Agent)
	ctx.Req.Form["asc_events"] = webhook_service.AgentHookEvents(ctx.Req.Form["asc_events"])
}

// wantsJSON reports whether the request came from a page script that reads a JSON answer.
func wantsJSON(ctx *context.Context) bool {
	return strings.Contains(ctx.Req.Header.Get("Accept"), "application/json")
}
