// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package user

import (
	gocontext "context"
	"time"

	issues_model "forgejo.org/models/issues"
	repo_model "forgejo.org/models/repo"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/base"
	"forgejo.org/services/ascinbox"
	"forgejo.org/services/authz"
)

// The inbox page (SPEC 20.8, PLAN unit C41c): `GET /-/asc/inbox` lists, for the signed-in user, the pull
// requests that need their attention, grouped in sections (whose turn it is), with their own pin and snooze
// marks. It is the web twin of `GET /api/v1/user/asc/inbox` and uses the same service (services/ascinbox).
//
// Trust: the user is always the signed-in doer. No query value names a user or a section filter that
// widens what is shown. The service checks read access to the pull requests of each repository, so one the
// user lost access to is never listed, and marks are read only for the doer.
const tplAscInbox base.TplName = "user/asc_inbox"

// AscInboxEntry is one pull request in the inbox of one user.
type AscInboxEntry = ascinbox.Entry

// AscInboxGroup is one section as shown.
type AscInboxGroup struct {
	Section issues_model.AscInboxSection
	Entries []*AscInboxEntry
}

// ascInboxLoad returns the entries of doer, each with its derived section. Tests replace it.
var ascInboxLoad = ascInboxLoadFromModels

// ascInboxLoadFromModels loads the entries with the shared service. A browser session carries the full rights of
// the user; only an access token narrows them.
func ascInboxLoadFromModels(ctx gocontext.Context, doer *user_model.User, now time.Time) ([]*AscInboxEntry, error) {
	return ascinbox.Load(ctx, doer, &authz.AllAccessAuthorizationReducer{}, now)
}

// ascInboxCanSee reports whether doer may read the pull requests of repo.
func ascInboxCanSee(ctx gocontext.Context, doer *user_model.User, repo *repo_model.Repository) bool {
	return ascinbox.CanRead(ctx, doer, &authz.AllAccessAuthorizationReducer{}, repo)
}

// ascInboxHead returns the head commit of pr, or "".
func ascInboxHead(ctx gocontext.Context, pr *issues_model.PullRequest) string {
	return ascinbox.HeadCommit(ctx, pr)
}

// ascInboxApproved reports whether pr meets the approvals of its branch rule.
func ascInboxApproved(ctx gocontext.Context, pr *issues_model.PullRequest) bool {
	return ascinbox.Approved(ctx, pr)
}

// ascInboxGroup applies the marks of the doer to entries and groups them in section order. A snoozed entry
// is left out and counted; inside a section pinned entries come first, then the most recently updated.
func ascInboxGroup(entries []*AscInboxEntry, states map[int64]*issues_model.AscInboxState, now time.Time) ([]AscInboxGroup, int) {
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
		out = append(out, AscInboxGroup{Section: e.Section, Entries: []*AscInboxEntry{e}})
	}
	return out, snoozed
}
