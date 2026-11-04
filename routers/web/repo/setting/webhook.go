// Copyright 2015 The Gogs Authors. All rights reserved.
// Copyright 2017 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package setting

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"path"

	"forgejo.org/models/db"
	"forgejo.org/models/perm"
	access_model "forgejo.org/models/perm/access"
	user_model "forgejo.org/models/user"
	"forgejo.org/models/webhook"
	"forgejo.org/modules/base"
	"forgejo.org/modules/git"
	"forgejo.org/modules/json"
	"forgejo.org/modules/setting"
	api "forgejo.org/modules/structs"
	"forgejo.org/modules/web/middleware"
	webhook_module "forgejo.org/modules/webhook"
	"forgejo.org/services/context"
	"forgejo.org/services/convert"
	webhook_service "forgejo.org/services/webhook"

	"code.forgejo.org/go-chi/binding"
)

const (
	tplHooks        base.TplName = "repo/settings/webhook/base"
	tplHookNew      base.TplName = "repo/settings/webhook/new"
	tplOrgHookNew   base.TplName = "org/settings/hook_new"
	tplUserHookNew  base.TplName = "user/settings/hook_new"
	tplAdminHookNew base.TplName = "admin/hook_new"
)

// WebhookList render web hooks list page
func WebhookList(ctx *context.Context) {
	ctx.Data["Title"] = ctx.Tr("repo.settings.hooks")
	ctx.Data["PageIsSettingsHooks"] = true
	ctx.Data["BaseLink"] = ctx.Repo.RepoLink + "/settings/hooks"
	ctx.Data["BaseLinkNew"] = ctx.Repo.RepoLink + "/settings/hooks"
	ctx.Data["WebhookList"] = webhook_service.List()
	ctx.Data["Description"] = ctx.Tr("asc_hooks.list_desc")
	ctx.Data["SettingsLead"] = ctx.Tr("asc_hooks.lead")

	ws, err := db.Find[webhook.Webhook](ctx, webhook.ListWebhookOptions{RepoID: ctx.Repo.Repository.ID})
	if err != nil {
		ctx.ServerError("GetWebhooksByRepoID", err)
		return
	}
	rows, err := webhook_service.ListRows(ctx, ws)
	if err != nil {
		ctx.ServerError("ListRows", err)
		return
	}
	ctx.Data["Webhooks"] = rows
	ctx.Data["AddDialog"] = webhook_service.AddDialogFor(ctx, ctx.Repo.Repository)

	ctx.HTML(http.StatusOK, tplHooks)
}

type ownerRepoCtx struct {
	OwnerID         int64
	RepoID          int64
	IsAdmin         bool
	IsSystemWebhook bool
	Link            string
	LinkNew         string
	NewTemplate     base.TplName
}

// getOwnerRepoCtx determines whether this is a repo, owner, or admin (both default and system) context.
func getOwnerRepoCtx(ctx *context.Context) (*ownerRepoCtx, error) {
	if ctx.Data["PageIsRepoSettings"] == true {
		return &ownerRepoCtx{
			RepoID:      ctx.Repo.Repository.ID,
			Link:        path.Join(ctx.Repo.RepoLink, "settings/hooks"),
			LinkNew:     path.Join(ctx.Repo.RepoLink, "settings/hooks"),
			NewTemplate: tplHookNew,
		}, nil
	}

	if ctx.Data["PageIsOrgSettings"] == true {
		return &ownerRepoCtx{
			OwnerID:     ctx.ContextUser.ID,
			Link:        path.Join(ctx.Org.OrgLink, "settings/hooks"),
			LinkNew:     path.Join(ctx.Org.OrgLink, "settings/hooks"),
			NewTemplate: tplOrgHookNew,
		}, nil
	}

	if ctx.Data["PageIsUserSettings"] == true {
		return &ownerRepoCtx{
			OwnerID:     ctx.Doer.ID,
			Link:        path.Join(setting.AppSubURL, "/user/settings/hooks"),
			LinkNew:     path.Join(setting.AppSubURL, "/user/settings/hooks"),
			NewTemplate: tplUserHookNew,
		}, nil
	}

	if ctx.Data["PageIsAdmin"] == true {
		return &ownerRepoCtx{
			IsAdmin:         true,
			IsSystemWebhook: ctx.Params(":configType") == "system-hooks",
			Link:            path.Join(setting.AppSubURL, "/admin/hooks"),
			LinkNew:         path.Join(setting.AppSubURL, "/admin/", ctx.Params(":configType")),
			NewTemplate:     tplAdminHookNew,
		}, nil
	}

	return nil, errors.New("unable to set OwnerRepo context")
}

// WebhookNew serves `GET {hooks}/{type}/new`, the page that adds a hook of one kind; it sends pushes and landed
// changes until other events are ticked.
func WebhookNew(ctx *context.Context) {
	orCtx, handler := newHookPage(ctx)
	if ctx.Written() {
		return
	}
	w := &webhook.Webhook{Type: ctx.Params(":type"), IsActive: true, HookEvent: &webhook_module.HookEvent{}}
	webhook_service.ChooseSent(w, []string{string(webhook_module.HookEventPush), string(webhook_module.HookEventChangeLanded)})
	showNewHook(ctx, w, handler.Metadata(w))
	ctx.HTML(http.StatusOK, orCtx.NewTemplate)
}

// newHookPage fills the page data the page that adds a hook of the kind in the address shares with its refused
// forms: the page it sits on, the kinds and the links. It answers 404 for a kind there is none of.
func newHookPage(ctx *context.Context) (*ownerRepoCtx, webhook_service.Handler) {
	orCtx, err := getOwnerRepoCtx(ctx)
	if err != nil {
		ctx.ServerError("getOwnerRepoCtx", err)
		return nil, nil
	}
	hookType := ctx.Params(":type")
	handler := webhook_service.FormHandler(hookType)
	if handler == nil {
		ctx.NotFound("GetWebhookHandler", nil)
		return nil, nil
	}

	ctx.Data["Title"] = ctx.Tr("repo.settings.add_webhook")
	if orCtx.IsAdmin && orCtx.IsSystemWebhook {
		ctx.Data["PageIsAdminSystemHooks"] = true
		ctx.Data["PageIsAdminSystemHooksNew"] = true
	} else if orCtx.IsAdmin {
		ctx.Data["PageIsAdminDefaultHooks"] = true
		ctx.Data["PageIsAdminDefaultHooksNew"] = true
	} else {
		ctx.Data["PageIsSettingsHooks"] = true
		ctx.Data["PageIsSettingsHooksNew"] = true
	}
	ctx.Data["HookType"] = hookType
	ctx.Data["WebhookHandler"] = handler
	ctx.Data["BaseLink"] = orCtx.LinkNew
	ctx.Data["BaseLinkNew"] = orCtx.LinkNew
	ctx.Data["WebhookList"] = webhook_service.List()
	return orCtx, handler
}

// showNewHook puts the hook w being added on the page, with the fields of its kind and the events it sends.
func showNewHook(ctx *context.Context, w *webhook.Webhook, meta any) {
	ctx.Data["Webhook"] = w
	ctx.Data["HookMetadata"] = meta
	ctx.Data["SendGroups"] = webhook_service.SendGroups(w)
	ctx.Data["HookHasSecret"] = webhook_service.HasSecret(w.Type)
}

// WebhookCreate serves `POST {hooks}/{type}/new`, the page that adds a hook: the fields of its kind as the Settings
// tab names them, asc_events, the events it sends, and active. A refused form shows the page again with what was
// sent, 422.
func WebhookCreate(ctx *context.Context) {
	orCtx, handler := newHookPage(ctx)
	if ctx.Written() {
		return
	}
	trimFormValue(ctx, "payload_url")
	fields := handler.UnmarshalForm(func(form any) {
		errs := binding.Bind(ctx.Req, form)
		middleware.Validate(errs, ctx.Data, form, ctx.Locale) // error checked below in ctx.HasError
	})

	w := &webhook.Webhook{
		RepoID:          orCtx.RepoID,
		URL:             fields.URL,
		HTTPMethod:      fields.HTTPMethod,
		IsActive:        fields.Active,
		Type:            ctx.Params(":type"),
		OwnerID:         orCtx.OwnerID,
		IsSystemWebhook: orCtx.IsSystemWebhook,
		HookEvent:       &webhook_module.HookEvent{BranchFilter: fields.BranchFilter},
	}
	if webhook_service.HasSecret(w.Type) {
		w.ContentType = fields.ContentType
		w.Secret = fields.Secret
	}
	webhook_service.ChooseSent(w, ctx.Req.Form["asc_events"])

	refused := ctx.HasError()
	if !refused && webhook_service.CheckPayloadURL(fields.URL) != nil {
		payloadURLRefused(ctx)
		refused = true
	}
	if refused {
		shown := *w
		shown.ID = -1 // the header is encrypted with the ID: one that never exists keeps it from matching a stored hook
		shown.SetHeaderAuthorization(fields.AuthorizationHeader)
		showNewHook(ctx, &shown, fields.Metadata)
		ctx.HTML(http.StatusUnprocessableEntity, orCtx.NewTemplate)
		return
	}

	if fields.Metadata != nil {
		meta, err := json.Marshal(fields.Metadata)
		if err != nil {
			ctx.ServerError("Marshal", err)
			return
		}
		w.Meta = string(meta)
	}
	if err := w.UpdateEvent(); err != nil {
		ctx.ServerError("UpdateEvent", err)
		return
	}
	if err := webhook.CreateWebhook(ctx, w, fields.AuthorizationHeader); err != nil {
		ctx.ServerError("CreateWebhook", err)
		return
	}

	ctx.Flash.Success(ctx.Tr("repo.settings.add_hook_success"))
	ctx.Redirect(orCtx.Link)
}

// payloadURLRefused marks the payload address field of a webhook form whose address the transport rule refused; the
// form says why under the field.
func payloadURLRefused(ctx *context.Context) {
	ctx.Data["Err_PayloadURL"] = true
	ctx.Data["PayloadURLRefused"] = true
}

func checkWebhook(ctx *context.Context) (*ownerRepoCtx, *webhook.Webhook) {
	orCtx, err := getOwnerRepoCtx(ctx)
	if err != nil {
		ctx.ServerError("getOwnerRepoCtx", err)
		return nil, nil
	}
	ctx.Data["BaseLink"] = orCtx.Link
	ctx.Data["BaseLinkNew"] = orCtx.LinkNew
	ctx.Data["WebhookList"] = webhook_service.List()

	var w *webhook.Webhook
	if orCtx.RepoID > 0 {
		w, err = webhook.GetWebhookByRepoID(ctx, orCtx.RepoID, ctx.ParamsInt64(":id"))
	} else if orCtx.OwnerID > 0 { // nosemgrep: forgejo-logic-suspicious-OwnerID-check (system users do not own webhooks)
		w, err = webhook.GetWebhookByOwnerID(ctx, orCtx.OwnerID, ctx.ParamsInt64(":id"))
	} else if orCtx.IsAdmin {
		w, err = webhook.GetSystemOrDefaultWebhook(ctx, ctx.ParamsInt64(":id"))
	}
	if err != nil || w == nil {
		if webhook.IsErrWebhookNotExist(err) {
			ctx.NotFound("GetWebhookByID", nil)
		} else {
			ctx.ServerError("GetWebhookByID", err)
		}
		return nil, nil
	}

	ctx.Data["HookType"] = w.Type

	if handler := webhook_service.GetWebhookHandler(w.Type); handler != nil {
		ctx.Data["HookMetadata"] = handler.Metadata(w)
		ctx.Data["WebhookHandler"] = handler
	}
	ctx.Data["SendGroups"] = webhook_service.SendGroups(w)
	ctx.Data["HookHasSecret"] = webhook_service.HasSecret(w.Type)
	if w.Type == webhook_module.ASCAGENT && ctx.Repo.Repository != nil {
		ctx.Data["AgentRoster"] = webhook_service.AgentRoster(ctx, ctx.Repo.Repository)
	}

	if err := loadDeliveries(ctx, w); err != nil {
		ctx.ServerError("loadDeliveries", err)
		return nil, nil
	}
	return orCtx, w
}

// loadDeliveries fills the two views of one webhook: which tab is open, whether the hook is failing, a page of its
// deliveries and the one delivery whose request or response is shown (the newest on the page unless one is asked for).
func loadDeliveries(ctx *context.Context, w *webhook.Webhook) error {
	page := max(ctx.FormInt("page"), 1)
	tasks, err := w.History(ctx, page)
	if err != nil {
		return err
	}
	count, err := webhook.CountHookTasks(ctx, w.ID)
	if err != nil {
		return err
	}
	streaks, err := webhook.FailingStreaks(ctx, []int64{w.ID}, webhook_service.StreakLimit)
	if err != nil {
		return err
	}
	deliveries := webhook_service.Deliveries(tasks)
	var selected *webhook_service.Delivery
	if uuid := ctx.FormString("delivery"); uuid != "" {
		for _, d := range deliveries {
			if d.UUID == uuid {
				selected = d
			}
		}
		if selected == nil {
			task, err := webhook.GetHookTaskByUUID(ctx, w.ID, uuid)
			if err != nil && !webhook.IsErrHookTaskNotExist(err) {
				return err
			}
			if task != nil {
				selected = &webhook_service.Delivery{HookTask: task}
			}
		}
	}
	if selected == nil && len(deliveries) > 0 {
		selected = deliveries[0]
	}

	pager := context.NewPagination(int(count), setting.Webhook.PagingNum, page, 5)
	pager.AddParamString("tab", "deliveries")
	ctx.Data["Page"] = pager
	ctx.Data["HookTab"] = "settings"
	if ctx.FormString("tab") == "deliveries" {
		ctx.Data["HookTab"] = "deliveries"
	}
	ctx.Data["HookFailing"] = streaks[w.ID] > 1
	ctx.Data["Deliveries"] = deliveries
	ctx.Data["DeliveryCount"] = count
	ctx.Data["Delivery"] = selected
	ctx.Data["DeliveryPart"] = "request"
	if ctx.FormString("part") == "response" {
		ctx.Data["DeliveryPart"] = "response"
	}
	ctx.Data["DeliveriesLink"] = fmt.Sprintf("%s?tab=deliveries&page=%d", ctx.Link, page)
	return nil
}

// WebhookEdit render editing web hook page
func WebhookEdit(ctx *context.Context) {
	ctx.Data["Title"] = ctx.Tr("repo.settings.update_webhook")
	ctx.Data["PageIsSettingsHooks"] = true
	ctx.Data["PageIsSettingsHooksEdit"] = true

	orCtx, w := checkWebhook(ctx)
	if ctx.Written() {
		return
	}
	ctx.Data["Webhook"] = w

	ctx.HTML(http.StatusOK, orCtx.NewTemplate)
}

// WebhookTest test if web hook is work fine
func WebhookTest(ctx *context.Context) {
	hookID := ctx.ParamsInt64(":id")
	w, err := webhook.GetWebhookByRepoID(ctx, ctx.Repo.Repository.ID, hookID)
	if err != nil {
		ctx.Flash.Error("GetWebhookByRepoID: " + err.Error())
		ctx.Status(http.StatusInternalServerError)
		return
	}

	// Grab latest commit or fake one if it's empty repository.
	commit := ctx.Repo.Commit
	if commit == nil {
		ghost := user_model.NewGhostUser()
		objectFormat := git.ObjectFormatFromName(ctx.Repo.Repository.ObjectFormatName)
		commit = &git.Commit{
			ID:            objectFormat.EmptyObjectID(),
			Author:        ghost.NewGitSig(),
			Committer:     ghost.NewGitSig(),
			CommitMessage: "This is a fake commit",
		}
	}

	apiUser := convert.ToUserWithAccessMode(ctx, ctx.Doer, perm.AccessModeNone)

	apiCommit := &api.PayloadCommit{
		ID:      commit.ID.String(),
		Message: commit.Message(),
		URL:     ctx.Repo.Repository.HTMLURL() + "/commit/" + url.PathEscape(commit.ID.String()),
		Author: &api.PayloadUser{
			Name:  commit.Author.Name,
			Email: commit.Author.Email,
		},
		Committer: &api.PayloadUser{
			Name:  commit.Committer.Name,
			Email: commit.Committer.Email,
		},
	}

	commitID := commit.ID.String()
	p := &api.PushPayload{
		Ref:          git.BranchPrefix + ctx.Repo.Repository.DefaultBranch,
		Before:       commitID,
		After:        commitID,
		CompareURL:   setting.AppURL + ctx.Repo.Repository.ComposeCompareURL(commitID, commitID),
		Commits:      []*api.PayloadCommit{apiCommit},
		TotalCommits: 1,
		HeadCommit:   apiCommit,
		Repo:         convert.ToRepo(ctx, ctx.Repo.Repository, access_model.Permission{AccessMode: perm.AccessModeNone}),
		Pusher:       apiUser,
		Sender:       apiUser,
	}
	if err := webhook_service.PrepareTestWebhook(ctx, w, p); err != nil {
		ctx.Flash.Error("PrepareWebhook: " + err.Error())
		ctx.Status(http.StatusInternalServerError)
	} else {
		ctx.Flash.Info(ctx.Tr("repo.settings.webhook.delivery.success"))
		ctx.Status(http.StatusOK)
	}
}

// WebhookReplay replays a webhook
func WebhookReplay(ctx *context.Context) {
	hookTaskUUID := ctx.Params(":uuid")

	orCtx, w := checkWebhook(ctx)
	if ctx.Written() {
		return
	}

	if err := webhook_service.ReplayHookTask(ctx, w, hookTaskUUID); err != nil {
		if webhook.IsErrHookTaskNotExist(err) {
			ctx.NotFound("ReplayHookTask", nil)
		} else {
			ctx.ServerError("ReplayHookTask", err)
		}
		return
	}

	ctx.Flash.Success(ctx.Tr("repo.settings.webhook.delivery.success"))
	// Replay from a list of webhooks comes back to that list
	ctx.RedirectToFirst(ctx.FormString("redirect_to"), fmt.Sprintf("%s/%d", orCtx.Link, w.ID))
}

// WebhookRegenerateSecret gives a webhook that signs its deliveries a new random secret and shows it once in the
// success message, so the receiving service can be updated with it.
func WebhookRegenerateSecret(ctx *context.Context) {
	orCtx, w := checkWebhook(ctx)
	if ctx.Written() {
		return
	}
	if !webhook_service.HasSecret(w.Type) {
		ctx.NotFound("WebhookRegenerateSecret", nil)
		return
	}
	secret := webhook_service.NewSecret()
	w.Secret = secret
	if err := webhook.UpdateWebhookSecret(ctx, w); err != nil {
		ctx.ServerError("UpdateWebhookSecret", err)
		return
	}
	ctx.Flash.Success(ctx.Tr("asc_hooks.secret_regenerated", secret))
	ctx.Redirect(fmt.Sprintf("%s/%d", orCtx.Link, w.ID))
}

// WebhookSetActive serves `POST {hooks}/{id}/active` with the field active (true or false): the switch of a list row,
// which pauses or resumes deliveries at once and keeps every other setting. It goes back to redirect_to, else to the hook.
func WebhookSetActive(ctx *context.Context) {
	orCtx, w := checkWebhook(ctx)
	if ctx.Written() {
		return
	}
	active := ctx.FormBool("active")
	if err := webhook.SetWebhookActive(ctx, w, active); err != nil {
		ctx.ServerError("SetWebhookActive", err)
		return
	}
	if active {
		ctx.Flash.Success(ctx.Tr("asc_hooks.resumed", w.URL))
	} else {
		ctx.Flash.Success(ctx.Tr("asc_hooks.paused", w.URL))
	}
	ctx.RedirectToFirst(ctx.FormString("redirect_to"), fmt.Sprintf("%s/%d", orCtx.Link, w.ID))
}

// WebhookDelete delete a webhook
func WebhookDelete(ctx *context.Context) {
	if err := webhook.DeleteWebhookByRepoID(ctx, ctx.Repo.Repository.ID, ctx.FormInt64("id")); err != nil {
		ctx.Flash.Error("DeleteWebhookByRepoID: " + err.Error())
	} else {
		ctx.Flash.Success(ctx.Tr("repo.settings.webhook_deletion_success"))
	}

	ctx.JSONRedirect(ctx.Repo.RepoLink + "/settings/hooks")
}
