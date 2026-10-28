// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package setting

import (
	"net/http"
	"slices"
	"strings"

	"forgejo.org/models/db"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/base"
	"forgejo.org/modules/optional"
	"forgejo.org/modules/setting"
	"forgejo.org/services/ascpresence"
	"forgejo.org/services/context"
	user_service "forgejo.org/services/user"
)

const tplSettingsPrivacy base.TplName = "user/settings/privacy"

func privacyLink() string { return setting.AppSubURL + "/user/settings/privacy" }

// Privacy renders the privacy page (board UserSettingsPrivacy): what others see, and the people the person blocks.
func Privacy(ctx *context.Context) {
	ctx.Data["Title"] = ctx.Tr("asc_work.sn_privacy")
	ctx.Data["PageIsSettingsPrivacy"] = true
	ctx.Data["Link"] = setting.AppSubURL + "/user/settings/blocked_users"
	ctx.Data["PrivacyLink"] = privacyLink()
	ctx.Data["ShowActivity"] = !ctx.Doer.KeepActivityPrivate
	ctx.Data["HideEmail"] = ctx.Doer.KeepEmailPrivate
	ctx.Data["ShowOnline"] = ascpresence.ShowOnline(ctx, ctx.Doer.ID)
	ctx.Data["AgentsReadProfile"] = user_model.AscAgentsMayReadProfile(ctx, ctx.Doer.ID)
	ctx.Data["NoreplyEmail"] = ctx.Doer.GetPlaceholderEmail()

	blocked, err := user_model.ListBlockedUsers(ctx, ctx.Doer.ID, db.ListOptions{})
	if err != nil {
		ctx.ServerError("ListBlockedUsers", err)
		return
	}
	ctx.Data["BlockedUsers"] = blocked
	ctx.HTML(http.StatusOK, tplSettingsPrivacy)
}

// PrivacyPost saves one switch of the privacy page. Each switch is its own form that saves when it changes.
func PrivacyPost(ctx *context.Context) {
	_ = ctx.Req.ParseForm()
	on := slices.Contains(ctx.Req.PostForm["value"], "1") // an unchecked switch sends only the hidden "0"
	opts := &user_service.UpdateOptions{}
	switch ctx.FormString("setting") {
	case "show_activity":
		opts.KeepActivityPrivate = optional.Some(!on)
	case "show_online":
		if err := ascpresence.SetShowOnline(ctx, ctx.Doer.ID, on); err != nil {
			ctx.ServerError("SetShowOnline", err)
			return
		}
		ctx.Flash.Success(ctx.Tr("settings.update_profile_success"))
		ctx.Redirect(privacyLink())
		return
	case "agents_profile":
		if err := user_model.SetAscAgentsMayReadProfile(ctx, ctx.Doer.ID, on); err != nil {
			ctx.ServerError("SetAscAgentsMayReadProfile", err)
			return
		}
		ctx.Flash.Success(ctx.Tr("settings.update_profile_success"))
		ctx.Redirect(privacyLink())
		return
	case "hide_email":
		opts.KeepEmailPrivate = optional.Some(on)
	default:
		ctx.Error(http.StatusBadRequest)
		return
	}
	if err := user_service.UpdateUser(ctx, ctx.Doer, opts); err != nil {
		ctx.ServerError("UpdateUser", err)
		return
	}
	ctx.Flash.Success(ctx.Tr("settings.update_profile_success"))
	ctx.Redirect(privacyLink())
}

// PrivacyBlock blocks a person by username.
func PrivacyBlock(ctx *context.Context) {
	name := strings.TrimSpace(strings.TrimPrefix(ctx.FormString("username"), "@"))
	u, err := user_model.GetUserByName(ctx, name)
	switch {
	case err != nil && user_model.IsErrUserNotExist(err):
		ctx.Flash.Error(ctx.Tr("asc_work.sp_block_not_found", name))
	case err != nil:
		ctx.ServerError("GetUserByName", err)
		return
	case u.IsOrganization() || !user_model.IsUserVisibleToViewer(ctx, u, ctx.Doer):
		// an organization and an account the person cannot see answer exactly like a name that does not exist
		ctx.Flash.Error(ctx.Tr("asc_work.sp_block_not_found", name))
	case u.ID == ctx.Doer.ID:
		ctx.Flash.Error(ctx.Tr("settings.user_block_yourself"))
	default:
		if err := user_service.BlockUser(ctx, ctx.Doer.ID, u.ID); err != nil {
			ctx.ServerError("BlockUser", err)
			return
		}
		ctx.Flash.Success(ctx.Tr("settings.user_block_success"))
	}
	ctx.Redirect(privacyLink())
}
