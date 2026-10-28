// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package setting

import (
	"fmt"
	"net/http"
	"slices"

	"forgejo.org/models/db"
	repo_model "forgejo.org/models/repo"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/base"
	"forgejo.org/modules/log"
	"forgejo.org/modules/optional"
	"forgejo.org/modules/setting"
	"forgejo.org/services/ascnotify"
	"forgejo.org/services/context"
	user_service "forgejo.org/services/user"
)

const tplSettingsNotifications base.TplName = "user/settings/notifications"

func notificationsLink() string { return setting.AppSubURL + "/user/settings/notifications" }

// ascTimeZones is the list the time zone select offers: the zones people pick most, each a valid IANA name.
var ascTimeZones = []string{
	"UTC", "Europe/London", "Europe/Lisbon", "Europe/Dublin", "Europe/Paris", "Europe/Berlin", "Europe/Madrid", "Europe/Rome",
	"Europe/Amsterdam", "Europe/Stockholm", "Europe/Warsaw", "Europe/Athens", "Europe/Istanbul", "Europe/Moscow",
	"Africa/Cairo", "Africa/Lagos", "Africa/Johannesburg", "Asia/Dubai", "Asia/Karachi", "Asia/Kolkata", "Asia/Dhaka",
	"Asia/Bangkok", "Asia/Singapore", "Asia/Shanghai", "Asia/Tokyo", "Asia/Seoul", "Australia/Perth", "Australia/Sydney",
	"Pacific/Auckland", "America/Sao_Paulo", "America/Argentina/Buenos_Aires", "America/Halifax", "America/New_York",
	"America/Chicago", "America/Denver", "America/Los_Angeles", "America/Anchorage", "Pacific/Honolulu",
}

type ascNotifyEvent struct {
	Key        string
	EmailOn    bool
	EmailReady bool // the hub sends email for this event, so the switch works
}

// Notifications renders the page (board UserSettingsNotifications): what reaches the person, email, quiet hours and
// the repositories they watch.
func Notifications(ctx *context.Context) {
	ctx.Data["Title"] = ctx.Tr("asc_work.sn_notifications")
	ctx.Data["PageIsSettingsNotifications"] = true
	ctx.Data["NotificationsLink"] = notificationsLink()

	prefs := ascnotify.Load(ctx, ctx.Doer.ID)
	events := make([]ascNotifyEvent, 0, len(ascnotify.Events))
	for _, e := range ascnotify.Events {
		ready := slices.Contains(ascnotify.EmailEvents, e)
		events = append(events, ascNotifyEvent{Key: e, EmailReady: ready, EmailOn: ready && prefs.EmailOn(e)})
	}
	ctx.Data["NotifyEvents"] = events
	ctx.Data["EmailPreference"] = ctx.Doer.EmailNotificationsPreference
	ctx.Data["QuietOn"] = prefs.Quiet.On
	from, until := prefs.Quiet.From, prefs.Quiet.Until
	if from == "" {
		from = ascnotify.DefaultFrom
	}
	if until == "" {
		until = ascnotify.DefaultUntil
	}
	ctx.Data["QuietFrom"], ctx.Data["QuietUntil"] = from, until
	ctx.Data["QuietHours"] = quarterHours()
	ctx.Data["TimeZone"] = prefs.TZ
	zones := ascTimeZones
	if prefs.TZ != "" && !slices.Contains(zones, prefs.TZ) {
		zones = append([]string{prefs.TZ}, zones...)
	}
	ctx.Data["TimeZones"] = zones

	repos, _, err := repo_model.SearchRepository(ctx, &repo_model.SearchRepoOptions{
		ListOptions: db.ListOptions{PageSize: 50, Page: 1},
		Actor:       ctx.Doer,
		OrderBy:     db.SearchOrderByRecentUpdated,
		Private:     true,
		WatchedByID: ctx.Doer.ID,
	})
	if err != nil {
		ctx.ServerError("SearchRepository", err)
		return
	}
	ctx.Data["WatchedRepos"] = repos
	ids := make([]int64, 0, len(repos))
	for _, r := range repos {
		ids = append(ids, r.ID)
	}
	if last, err := user_model.LastAscNotified(ctx, ctx.Doer.ID, ids); err == nil {
		ctx.Data["LastNotified"] = last
	}
	ctx.HTML(http.StatusOK, tplSettingsNotifications)
}

// quarterHours lists the times of day the quiet hours can start and end at, every half hour.
func quarterHours() []string {
	out := make([]string, 0, 48)
	for h := range 24 {
		for _, m := range []int{0, 30} {
			out = append(out, fmt.Sprintf("%02d:%02d", h, m))
		}
	}
	return out
}

// NotificationsPost saves one part of the page. Each control is its own form that saves when it changes.
func NotificationsPost(ctx *context.Context) {
	_ = ctx.Req.ParseForm()
	prefs := ascnotify.Load(ctx, ctx.Doer.ID)
	switch ctx.FormString("section") {
	case "email_event":
		event := ctx.FormString("event")
		if !slices.Contains(ascnotify.EmailEvents, event) {
			ctx.Error(http.StatusBadRequest)
			return
		}
		prefs.EmailOff = slices.DeleteFunc(prefs.EmailOff, func(e string) bool { return e == event })
		if !slices.Contains(ctx.Req.PostForm["value"], "1") {
			prefs.EmailOff = append(prefs.EmailOff, event)
		}
	case "email_mode":
		preference := ctx.FormString("preference")
		if !slices.Contains([]string{user_model.EmailNotificationsEnabled, user_model.EmailNotificationsOnMention, user_model.EmailNotificationsDisabled}, preference) {
			ctx.Error(http.StatusBadRequest)
			return
		}
		if err := user_service.UpdateUser(ctx, ctx.Doer, &user_service.UpdateOptions{EmailNotificationsPreference: optional.Some(preference)}); err != nil {
			ctx.ServerError("UpdateUser", err)
			return
		}
		ctx.Flash.Success(ctx.Tr("settings.update_profile_success"))
		ctx.Redirect(notificationsLink())
		return
	case "quiet":
		prefs.Quiet.On = slices.Contains(ctx.Req.PostForm["on"], "1")
		prefs.Quiet.From = ctx.FormString("from")
		prefs.Quiet.Until = ctx.FormString("until")
		prefs.TZ = ctx.FormString("tz")
	default:
		ctx.Error(http.StatusBadRequest)
		return
	}
	if err := ascnotify.Save(ctx, ctx.Doer.ID, prefs); err != nil {
		log.Warn("saving the notification choice of user %d: %v", ctx.Doer.ID, err)
		ctx.Flash.Error(err.Error())
	} else {
		ctx.Flash.Success(ctx.Tr("settings.update_profile_success"))
	}
	ctx.Redirect(notificationsLink())
}

// ascTimeZonesWith is the list of the time zone selects, with the zone the person has already chosen kept in it.
func ascTimeZonesWith(chosen string) []string {
	if chosen != "" && !slices.Contains(ascTimeZones, chosen) {
		return append([]string{chosen}, ascTimeZones...)
	}
	return ascTimeZones
}
