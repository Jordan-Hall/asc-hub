// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package user

import (
	gocontext "context"
	"net/http"
	"time"

	issues_model "forgejo.org/models/issues"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/base"
	"forgejo.org/modules/setting"
	"forgejo.org/services/ascinbox"
	"forgejo.org/services/authz"
	"forgejo.org/services/context"
)

// The inbox page (SPEC 20.8, PLAN unit C41c): `GET /-/asc/inbox` lists, for the signed-in user, the pull
// requests that need their attention, grouped in sections (whose turn it is), with their own pin and snooze
// marks. It is the web twin of `GET /api/v1/user/asc/inbox` and uses the same service (services/ascinbox).
//
// Trust: the user is always the signed-in doer. No query value names a user or a section filter that
// widens what is shown. The service checks read access to the pull requests of each repository, so one the
// user lost access to is never listed, and marks are read only for the doer.
const tplAscInbox base.TplName = "user/asc_inbox"

// AscInboxGroup is one section as shown.
type AscInboxGroup struct {
	Section issues_model.AscInboxSection
	Entries []*ascinbox.Entry
}

// ascInboxLoad returns the entries of doer, each with its derived section. Tests replace it.
var ascInboxLoad = func(ctx gocontext.Context, doer *user_model.User, now time.Time) ([]*ascinbox.Entry, error) {
	// A browser session carries the full rights of the user; only an access token narrows them.
	return ascinbox.Load(ctx, doer, &authz.AllAccessAuthorizationReducer{}, now)
}

// ascInboxGroup applies the marks of the doer to entries and groups them in section order. A snoozed entry
// is left out and counted; inside a section pinned entries come first, then the most recently updated.
func ascInboxGroup(entries []*ascinbox.Entry, states map[int64]*issues_model.AscInboxState, now time.Time) ([]AscInboxGroup, int) {
	arranged := ascinbox.Arrange(entries, states, now, ascinbox.Options{IncludeSnoozed: true})
	var out []AscInboxGroup
	snoozed := 0
	for _, e := range arranged {
		if !e.SnoozedUntil.IsZero() {
			snoozed++
			continue
		}
		if n := len(out); n > 0 && out[n-1].Section == e.Section {
			out[n-1].Entries = append(out[n-1].Entries, e)
			continue
		}
		out = append(out, AscInboxGroup{Section: e.Section, Entries: []*ascinbox.Entry{e}})
	}
	return out, snoozed
}

// AscInbox serves `GET /-/asc/inbox`.
func AscInbox(ctx *context.Context) {
	if !setting.Asc.Enabled || ctx.Doer == nil {
		ctx.NotFound("AscInbox", nil)
		return
	}
	ctx.Data["Title"] = ctx.Tr("user.asc_inbox")
	ctx.Data["PageIsAscInbox"] = true

	now := time.Now()
	entries, err := ascInboxLoad(ctx, ctx.Doer, now)
	if err != nil {
		ctx.ServerError("ascInboxLoad", err)
		return
	}
	ids := make([]int64, 0, len(entries))
	for _, e := range entries {
		ids = append(ids, e.PullID)
	}
	states, err := issues_model.ListAscInboxStates(ctx, ctx.Doer.ID, ids)
	if err != nil {
		ctx.ServerError("ListAscInboxStates", err)
		return
	}
	groups, snoozed := ascInboxGroup(entries, states, now)
	ctx.Data["AscInboxGroups"] = groups
	ctx.Data["AscInboxSnoozed"] = snoozed
	ctx.HTML(http.StatusOK, tplAscInbox)
}
