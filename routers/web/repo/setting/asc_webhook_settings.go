// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package setting

import (
	"fmt"
	"net/http"
	"strings"

	"forgejo.org/models/webhook"
	"forgejo.org/modules/json"
	"forgejo.org/modules/util"
	"forgejo.org/modules/web/middleware"
	webhook_module "forgejo.org/modules/webhook"
	"forgejo.org/services/context"
	webhook_service "forgejo.org/services/webhook"

	"code.forgejo.org/go-chi/binding"
)

// WebhookSaveSettings serves `POST {hooks}/{id}/settings`, the Settings tab of one hook, which is sent whole when a
// field changes: the fields of the hook's kind as its add form names them (payload_url, http_method, content_type,
// secret, branch_filter, authorization_header and the fields of a chat or build host), for an ASC agent hook agent,
// target and task instead, and asc_events, the ASC events it sends. The Active switch is kept. The page's script asks
// for JSON and gets {"ok":true}, or 422 with the field and the message to show under it; a form sent without script
// goes back to the hook with the message.
func WebhookSaveSettings(ctx *context.Context) {
	orCtx, w := checkWebhook(ctx)
	if ctx.Written() {
		return
	}
	back := fmt.Sprintf("%s/%d", orCtx.Link, w.ID)
	refuse := func(field, msg string) {
		if wantsJSON(ctx) {
			ctx.JSON(http.StatusUnprocessableEntity, map[string]string{"field": field, "error": msg})
			return
		}
		ctx.Flash.Error(msg)
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

// saveSendSettings reads the fields of a hook that sends requests into w through the form of its kind: the address
// (or the fields Telegram, Matrix and Packagist work it out from), the fields of its kind, the content type and the
// secret of a hook that signs its deliveries (an empty secret keeps the stored one), the HTTP method, the branch filter
// and the Authorization header. It returns false when it refused a field.
func saveSendSettings(ctx *context.Context, w *webhook.Webhook, refuse func(field, msg string)) bool {
	handler := webhook_service.FormHandler(w.Type)
	if handler == nil {
		ctx.NotFound("FormHandler", nil)
		return false
	}
	trimFormValue(ctx, "payload_url")
	field, msg := "", ""
	fields := handler.UnmarshalForm(func(form any) {
		errs := binding.Bind(ctx.Req, form)
		if len(errs) == 0 {
			return
		}
		data := map[string]any{}
		middleware.Validate(errs, data, form, ctx.Locale)
		msg = fmt.Sprint(data["ErrorMsg"])
		if len(errs[0].FieldNames) > 0 {
			field = util.ToSnakeCase(errs[0].FieldNames[0])
		}
	})
	if field == "payload_url" {
		msg = ctx.Locale.TrString("asc_hooks.url_invalid")
	}
	if msg != "" {
		refuse(field, msg)
		return false
	}
	if webhook_service.CheckPayloadURL(fields.URL) != nil {
		field = "payload_url"
		if w.Type == webhook_module.MATRIX {
			field = "homeserver_url"
		}
		refuse(field, ctx.Locale.TrString("asc_hooks.http_refused"))
		return false
	}
	meta := []byte{}
	if fields.Metadata != nil {
		var err error
		if meta, err = json.Marshal(fields.Metadata); err != nil {
			ctx.ServerError("Marshal", err)
			return false
		}
	}
	w.URL = fields.URL
	w.HTTPMethod = fields.HTTPMethod
	if webhook_service.HasSecret(w.Type) {
		w.ContentType = fields.ContentType
		if fields.Secret != "" {
			w.Secret = fields.Secret
		}
	}
	w.BranchFilter = fields.BranchFilter
	w.SetHeaderAuthorization(fields.AuthorizationHeader)
	w.Meta = string(meta)
	return true
}

// trimFormValue trims the spaces around the form field name, in the form a multipart body is bound from too.
func trimFormValue(ctx *context.Context, name string) {
	v := strings.TrimSpace(ctx.FormString(name))
	if _, ok := ctx.Req.Form[name]; ok {
		ctx.Req.Form[name] = []string{v}
	}
	if mf := ctx.Req.MultipartForm; mf != nil {
		if _, ok := mf.Value[name]; ok {
			mf.Value[name] = []string{v}
		}
	}
}

// saveAgentSettings reads the fields of an ASC agent hook into w: the agent, the branch and the task. The person
// who saves them is the one the runs are made for from then on, so the runs never do more than that person may.
// It writes the refusal when it refused one.
func saveAgentSettings(ctx *context.Context, w *webhook.Webhook, refuse func(field, msg string)) {
	m, field := webhook_service.CheckAgentMeta(webhook_service.AgentMeta{
		Agent:  ctx.FormString("agent"),
		Target: ctx.FormString("target"),
		Task:   ctx.FormString("task"),
		RunAs:  ctx.Doer.ID,
	})
	if field != "" {
		refuse(field, ctx.Locale.TrString("asc_hooks.agent_"+field+"_invalid"))
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
