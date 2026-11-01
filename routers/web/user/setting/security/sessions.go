// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package security

import (
	"strings"

	auth_model "forgejo.org/models/auth"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/timeutil"
	"forgejo.org/services/context"
)

// BrowserSessionRow is one signed-in browser as the security page and the administration show it.
type BrowserSessionRow struct {
	ID         int64
	Browser    string
	System     string
	IP         string
	Current    bool
	Created    timeutil.TimeStamp
	LastActive timeutil.TimeStamp
}

// loadBrowserSessions puts the signed-in browsers of the doer on the page, this one first.
func loadBrowserSessions(ctx *context.Context) {
	rows, err := auth_model.ListBrowserSessions(ctx, ctx.Doer.ID)
	if err != nil {
		ctx.ServerError("ListBrowserSessions", err)
		return
	}
	ctx.Data["BrowserSessions"] = BrowserSessionRows(rows, auth_model.HashSessionID(ctx.Session.ID()))
}

// BrowserSessionRows describes the browsers of rows, the one with session hash current first and marked.
func BrowserSessionRows(rows []*auth_model.BrowserSession, current string) []BrowserSessionRow {
	out := make([]BrowserSessionRow, 0, len(rows))
	for _, r := range rows {
		browser, system := describeUserAgent(r.UserAgent)
		row := BrowserSessionRow{
			ID: r.ID, Browser: browser, System: system, IP: r.IP, Current: r.SIDHash == current,
			Created: r.CreatedUnix, LastActive: r.LastActiveUnix,
		}
		if row.Current {
			out = append([]BrowserSessionRow{row}, out...)
		} else {
			out = append(out, row)
		}
	}
	return out
}

// RevokeBrowserSession serves `POST /user/settings/security/sessions/revoke` with the field id: it signs that browser
// out. This browser is signed out with the sign-out button, not here.
func RevokeBrowserSession(ctx *context.Context) {
	id := ctx.FormInt64("id")
	rows, err := auth_model.ListBrowserSessions(ctx, ctx.Doer.ID)
	if err != nil {
		ctx.ServerError("ListBrowserSessions", err)
		return
	}
	current := auth_model.HashSessionID(ctx.Session.ID())
	for _, r := range rows {
		if r.ID == id && r.SIDHash == current {
			ctx.Flash.Error(ctx.Tr("settings.browser_sessions_this_one"))
			ctx.Redirect(setting.AppSubURL + "/user/settings/security#sp-sessions")
			return
		}
	}
	found, err := auth_model.RevokeBrowserSession(ctx, ctx.Doer.ID, id)
	if err != nil {
		ctx.ServerError("RevokeBrowserSession", err)
		return
	}
	if found {
		ctx.Flash.Success(ctx.Tr("settings.browser_sessions_revoked"))
	} else {
		ctx.Flash.Error(ctx.Tr("settings.browser_sessions_gone"))
	}
	ctx.Redirect(setting.AppSubURL + "/user/settings/security#sp-sessions")
}

// RevokeOtherBrowserSessions serves `POST /user/settings/security/sessions/revoke_others`: it signs every other
// browser out.
func RevokeOtherBrowserSessions(ctx *context.Context) {
	n, err := auth_model.RevokeOtherBrowserSessions(ctx, ctx.Doer.ID, ctx.Session.ID())
	if err != nil {
		ctx.ServerError("RevokeOtherBrowserSessions", err)
		return
	}
	ctx.Flash.Success(ctx.TrN(n, "settings.browser_sessions_revoked_others_1", "settings.browser_sessions_revoked_others_n", n))
	ctx.Redirect(setting.AppSubURL + "/user/settings/security#sp-sessions")
}

// describeUserAgent names the browser and the system of a User-Agent header, in the words people know them by.
func describeUserAgent(ua string) (browser, system string) {
	switch {
	case strings.Contains(ua, "Edg/"):
		browser = "Edge"
	case strings.Contains(ua, "OPR/"):
		browser = "Opera"
	case strings.Contains(ua, "Firefox/"):
		browser = "Firefox"
	case strings.Contains(ua, "Chrome/") || strings.Contains(ua, "CriOS/"):
		browser = "Chrome"
	case strings.Contains(ua, "Safari/"):
		browser = "Safari"
	case ua == "":
		browser = ""
	default:
		browser, _, _ = strings.Cut(ua, "/")
	}
	switch {
	case strings.Contains(ua, "iPhone"):
		system = "iPhone"
	case strings.Contains(ua, "iPad"):
		system = "iPad"
	case strings.Contains(ua, "Android"):
		system = "Android"
	case strings.Contains(ua, "Windows"):
		system = "Windows"
	case strings.Contains(ua, "Mac OS X") || strings.Contains(ua, "Macintosh"):
		system = "macOS"
	case strings.Contains(ua, "CrOS"):
		system = "ChromeOS"
	case strings.Contains(ua, "Linux"):
		system = "Linux"
	}
	return browser, system
}
