// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package setting

import (
	"encoding/json"
	"errors"
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
	"forgejo.org/modules/webpush"
	"forgejo.org/services/ascnotify"
	"forgejo.org/services/ascpush"
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
	PushOn     bool
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
		events = append(events, ascNotifyEvent{Key: e, EmailReady: ready, EmailOn: ready && prefs.EmailOn(e), PushOn: prefs.PushOn(e)})
	}
	ctx.Data["NotifyEvents"] = events
	ctx.Data["EmailPreference"] = ctx.Doer.EmailNotificationsPreference
	ctx.Data["DigestOn"] = prefs.Digest
	if keys, err := ascpush.VAPID(ctx); err == nil {
		ctx.Data["PushKey"] = webpush.EncodeKey(keys.Public())
	} else {
		log.Error("asc push: VAPID key: %v", err)
	}
	subs, _ := user_model.ListAscPushSubscriptions(ctx, ctx.Doer.ID)
	ctx.Data["PushDevices"] = len(subs)
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
	case "push_event":
		event := ctx.FormString("event")
		if !slices.Contains(ascnotify.Events, event) {
			ctx.Error(http.StatusBadRequest)
			return
		}
		prefs.PushOff = slices.DeleteFunc(prefs.PushOff, func(e string) bool { return e == event })
		if !slices.Contains(ctx.Req.PostForm["value"], "1") {
			prefs.PushOff = append(prefs.PushOff, event)
		}
	case "digest":
		prefs.Digest = slices.Contains(ctx.Req.PostForm["on"], "1")
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

// ascPushBody is what a browser gives when it subscribes (PushSubscription.toJSON()).
type ascPushBody struct {
	Endpoint string `json:"endpoint"`
	Keys     struct {
		P256dh string `json:"p256dh"`
		Auth   string `json:"auth"`
	} `json:"keys"`
}

// PushSubscribe stores the subscription of the browser of the person. The endpoint is checked for being a public https
// address, the keys for being what a browser makes, and a person has a few browsers at most.
func PushSubscribe(ctx *context.Context) {
	var body ascPushBody
	if err := json.NewDecoder(http.MaxBytesReader(ctx.Resp, ctx.Req.Body, 8192)).Decode(&body); err != nil {
		ctx.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request"})
		return
	}
	if err := user_model.ValidateAscPushEndpoint(body.Endpoint); err != nil {
		ctx.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if err := webpush.ValidateSubscriptionKeys(body.Keys.P256dh, body.Keys.Auth); err != nil {
		ctx.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if err := user_model.SaveAscPushSubscription(ctx, ctx.Doer.ID, body.Endpoint, body.Keys.P256dh, body.Keys.Auth); err != nil {
		if errors.Is(err, user_model.ErrTooManyPushSubscriptions) {
			ctx.JSON(http.StatusConflict, map[string]string{"error": err.Error()})
			return
		}
		ctx.ServerError("SaveAscPushSubscription", err)
		return
	}
	ctx.JSON(http.StatusCreated, map[string]bool{"ok": true})
}

// PushUnsubscribe removes the subscription of a browser of the person.
func PushUnsubscribe(ctx *context.Context) {
	var body struct {
		Endpoint string `json:"endpoint"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(ctx.Resp, ctx.Req.Body, 4096)).Decode(&body); err != nil {
		ctx.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request"})
		return
	}
	if _, err := user_model.DeleteAscPushSubscription(ctx, ctx.Doer.ID, body.Endpoint); err != nil {
		ctx.ServerError("DeleteAscPushSubscription", err)
		return
	}
	ctx.JSON(http.StatusOK, map[string]bool{"ok": true})
}
