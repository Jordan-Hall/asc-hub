// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package setting

import (
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

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
	ctx.Data["ShowLocalTime"] = ascpresence.ShowLocalTime(ctx, ctx.Doer.ID)
	ctx.Data["AgentsReadProfile"] = user_model.AscAgentsMayReadProfile(ctx, ctx.Doer.ID)
	ctx.Data["NoreplyEmail"] = ctx.Doer.GetPlaceholderEmail()

	blocked, err := user_model.ListBlockedUsers(ctx, ctx.Doer.ID, db.ListOptions{})
	if err != nil {
		ctx.ServerError("ListBlockedUsers", err)
		return
	}
	ctx.Data["BlockedUsers"] = blocked
	ctx.Data["AscUndoUnblock"] = undoUnblockOffer(ctx, time.Now())
	ctx.Data["AscUndoSwitch"] = undoSwitchOffer(ctx, time.Now(), map[string]bool{
		"show_activity":   ctx.Data["ShowActivity"].(bool),
		"show_online":     ctx.Data["ShowOnline"].(bool),
		"show_local_time": ctx.Data["ShowLocalTime"].(bool),
		"agents_profile":  ctx.Data["AgentsReadProfile"].(bool),
		"hide_email":      ctx.Data["HideEmail"].(bool),
	})
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
		ctx.Redirect(switchedLink(ctx.FormString("setting"), time.Now()))
		return
	case "show_local_time":
		if err := ascpresence.SetShowLocalTime(ctx, ctx.Doer.ID, on); err != nil {
			ctx.ServerError("SetShowLocalTime", err)
			return
		}
		ctx.Redirect(switchedLink(ctx.FormString("setting"), time.Now()))
		return
	case "agents_profile":
		if err := user_model.SetAscAgentsMayReadProfile(ctx, ctx.Doer.ID, on); err != nil {
			ctx.ServerError("SetAscAgentsMayReadProfile", err)
			return
		}
		ctx.Redirect(switchedLink(ctx.FormString("setting"), time.Now()))
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
	ctx.Redirect(switchedLink(ctx.FormString("setting"), time.Now()))
}

// switchLabels names each switch of the privacy page by the locale key of its label.
var switchLabels = map[string]string{
	"show_activity":   "asc_work.sp_show_activity",
	"show_online":     "asc_work.sp_show_online",
	"show_local_time": "asc_work.sp_show_local_time",
	"agents_profile":  "asc_work.sp_agents_profile",
	"hide_email":      "asc_work.sp_hide_email",
}

// switchedLink is the privacy page after a switch saved: the page then says what changed and offers Undo for ten seconds.
func switchedLink(name string, at time.Time) string {
	return privacyLink() + "?" + url.Values{"switched": {name}, "t": {strconv.FormatInt(at.Unix(), 10)}}.Encode()
}

// SwitchUndo is the offer to put a switch back: the switch, its label, whether it is on now, and the value Undo sends.
type SwitchUndo struct {
	Setting string
	Label   string
	On      bool
}

// undoSwitchOffer is the switch the page offers to put back, or nil: the address names a known switch that saved less
// than undoWindow ago. Undo sends the opposite of the value the switch has now, so it is judged on what is stored.
func undoSwitchOffer(ctx *context.Context, now time.Time, current map[string]bool) *SwitchUndo {
	name := ctx.FormTrim("switched")
	label, ok := switchLabels[name]
	if !ok {
		return nil
	}
	at, err := strconv.ParseInt(ctx.FormString("t"), 10, 64)
	if err != nil {
		return nil
	}
	if age := now.Sub(time.Unix(at, 0)); age < 0 || age > undoWindow {
		return nil
	}
	return &SwitchUndo{Setting: name, Label: label, On: current[name]}
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

// undoWindow is how long after an unblock the privacy page offers to block the person again.
const undoWindow = 10 * time.Second

// undoUnblockOffer is the login the page offers to block again, or "": the address names a person who is not blocked now,
// who is visible to the viewer, and the unblock happened less than undoWindow ago.
func undoUnblockOffer(ctx *context.Context, now time.Time) string {
	name := ctx.FormTrim("undo")
	at, err := strconv.ParseInt(ctx.FormString("t"), 10, 64)
	if name == "" || err != nil {
		return ""
	}
	if age := now.Sub(time.Unix(at, 0)); age < 0 || age > undoWindow {
		return ""
	}
	u, err := user_model.GetUserByName(ctx, name)
	if err != nil || u.IsOrganization() || u.ID == ctx.Doer.ID || user_model.IsBlocked(ctx, ctx.Doer.ID, u.ID) || !user_model.IsUserVisibleToViewer(ctx, u, ctx.Doer) {
		return ""
	}
	return u.Name
}
