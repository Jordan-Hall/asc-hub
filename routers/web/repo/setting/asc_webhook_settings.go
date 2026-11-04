// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package setting

import (
	"fmt"
	"net/http"
	"strings"

	"forgejo.org/models/webhook"
	"forgejo.org/modules/validation"
	"forgejo.org/services/context"
	webhook_service "forgejo.org/services/webhook"
)

// WebhookSaveSettings serves `POST {hooks}/{id}/settings`, the Settings tab of one hook, which saves each field when it
// changes: payload_url, for a hook that signs its deliveries content_type (1 json, 2 form) and secret (empty keeps
// the stored one), and asc_events, the ASC events it sends. Every other setting of the hook is kept. The page's script
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

	url := strings.TrimSpace(ctx.FormString("payload_url"))
	if !validation.IsValidURL(url) {
		refuse("payload_url", "asc_hooks.url_invalid")
		return
	}
	if webhook_service.CheckPayloadURL(url) != nil {
		refuse("payload_url", "asc_hooks.http_refused")
		return
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

// wantsJSON reports whether the request came from a page script that reads a JSON answer.
func wantsJSON(ctx *context.Context) bool {
	return strings.Contains(ctx.Req.Header.Get("Accept"), "application/json")
}
