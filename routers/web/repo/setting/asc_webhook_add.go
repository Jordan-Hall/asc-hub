// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package setting

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"forgejo.org/models/webhook"
	"forgejo.org/modules/json"
	"forgejo.org/modules/validation"
	webhook_module "forgejo.org/modules/webhook"
	"forgejo.org/services/context"
	webhook_service "forgejo.org/services/webhook"
)

// Kinds of the Add webhook dialog (templates/repo/settings/webhook/add_dialog.tmpl).
const (
	addKindGeneric = "generic"
	addKindChat    = "chat"
	addKindAgent   = "agent"
)

// chatHookType is the chat webhook type for a payload address: Discord for its own hosts, else the Slack format,
// which Slack, Mattermost and Rocket.Chat take.
func chatHookType(addr string) webhook_module.HookType {
	u, err := url.Parse(addr)
	if err == nil {
		switch strings.ToLower(u.Hostname()) {
		case "discord.com", "discordapp.com", "ptb.discord.com", "canary.discord.com":
			return webhook_module.DISCORD
		}
	}
	return webhook_module.SLACK
}

// WebhookAdd serves `POST {hooks}/add`, the Add webhook dialog of a repository, organization or user. kind is
// generic, chat or agent. A generic hook gets the JSON format, POST, a new secret (shown once on the hook's page) and
// the change.landed event; a chat hook posts change.landed to the channel of its address; an agent hook (only in a
// repository asc serves) runs agent with task on change.landed, for the default branch, as the person who adds it.
// The page's script asks for JSON and gets {"redirect": hook page}, or 422 with the field and its message; a form
// sent without script goes to the hook, or back to the list with the message.
func WebhookAdd(ctx *context.Context) {
	orCtx, err := getOwnerRepoCtx(ctx)
	if err != nil {
		ctx.ServerError("getOwnerRepoCtx", err)
		return
	}
	refuse := func(field, key string) {
		if wantsJSON(ctx) {
			ctx.JSON(http.StatusUnprocessableEntity, map[string]string{"field": field, "error": ctx.Locale.TrString(key)})
			return
		}
		ctx.Flash.Error(ctx.Tr(key))
		ctx.Redirect(orCtx.Link)
	}

	w := &webhook.Webhook{
		RepoID:          orCtx.RepoID,
		OwnerID:         orCtx.OwnerID,
		IsSystemWebhook: orCtx.IsSystemWebhook,
		IsActive:        true,
		HTTPMethod:      http.MethodPost,
		ContentType:     webhook.ContentTypeJSON,
		HookEvent:       &webhook_module.HookEvent{ChooseEvents: true},
	}
	w.SetAscEvents([]string{string(webhook_module.HookEventChangeLanded)})
	secret := ""

	switch kind := ctx.FormString("kind"); kind {
	case addKindGeneric, addKindChat:
		addr := strings.TrimSpace(ctx.FormString("payload_url"))
		if !validation.IsValidURL(addr) {
			refuse("payload_url", "asc_hooks.url_invalid")
			return
		}
		if webhook_service.CheckPayloadURL(addr) != nil {
			refuse("payload_url", "asc_hooks.http_refused")
			return
		}
		w.URL = addr
		if kind == addKindGeneric {
			w.Type = webhook_module.FORGEJO
			secret = webhook_service.NewSecret()
			w.Secret = secret
		} else {
			w.Type = chatHookType(addr)
			var meta any = &webhook_service.SlackMeta{}
			if w.Type == webhook_module.DISCORD {
				meta = &webhook_service.DiscordMeta{}
			}
			b, err := json.Marshal(meta)
			if err != nil {
				ctx.ServerError("Marshal", err)
				return
			}
			w.Meta = string(b)
		}
	case addKindAgent:
		if orCtx.RepoID == 0 || !webhook_service.AgentHookAllowed(ctx.Repo.Repository) {
			refuse("kind", "asc_hooks.add_agent_unavailable")
			return
		}
		m, field := webhook_service.CheckAgentMeta(webhook_service.AgentMeta{
			Agent:  ctx.FormString("agent"),
			Target: ctx.Repo.Repository.DefaultBranch,
			Task:   ctx.FormString("task"),
			RunAs:  ctx.Doer.ID,
		})
		if field != "" {
			refuse(field, "asc_hooks.agent_"+field+"_invalid")
			return
		}
		b, err := json.Marshal(m)
		if err != nil {
			ctx.ServerError("Marshal", err)
			return
		}
		w.Type, w.URL, w.Meta = webhook_module.ASCAGENT, webhook_service.AgentURL(m.Agent), string(b)
	default:
		refuse("kind", "asc_hooks.add_kind_invalid")
		return
	}

	if err := w.UpdateEvent(); err != nil {
		ctx.ServerError("UpdateEvent", err)
		return
	}
	if err := webhook.CreateWebhook(ctx, w, ""); err != nil {
		ctx.ServerError("CreateWebhook", err)
		return
	}
	if secret != "" {
		ctx.Flash.Success(ctx.Tr("asc_hooks.added_secret", secret))
	} else {
		ctx.Flash.Success(ctx.Tr("repo.settings.add_hook_success"))
	}
	link := fmt.Sprintf("%s/%d", orCtx.Link, w.ID)
	if wantsJSON(ctx) {
		ctx.JSON(http.StatusOK, map[string]string{"redirect": link})
		return
	}
	ctx.Redirect(link)
}
