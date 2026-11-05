// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package user

import (
	gocontext "context"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	activities_model "forgejo.org/models/activities"
	"forgejo.org/models/db"
	issues_model "forgejo.org/models/issues"
	"forgejo.org/models/perm/access"
	repo_model "forgejo.org/models/repo"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/base"
	"forgejo.org/modules/log"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/translation"
	"forgejo.org/services/context"
)

// The notifications popover of the header bell (board Shell, 03): the newest notifications of the signed-in person,
// the ones of the hub (issues, pull requests, transfers) and the events asc serve reported (agent runs, checks, the
// merge queue, restacks, policy), with the number not yet read, Mark all read and Open inbox. The bell stays a link
// to /notifications for a page without script.

const (
	ascBellMax = 6 // the most notifications the popover lists

	tplAscBell base.TplName = "user/notification/asc_bell"
)

// AscBellItem is one notification of the popover.
type AscBellItem struct {
	Actor    *user_model.User // the person of the hub who did it; nil for an event asc reported
	Initials string           // the mark of an event asc reported
	Name     string           // bold before Text: the name of Actor
	Text     string
	Sub      string
	Link     string
	When     time.Time
	Unread   bool
}

// ascBellVerb is the words of a notification of an issue or pull request: what the comment, or the issue itself when
// there is no comment, did.
func ascBellVerb(ctx gocontext.Context, l translation.Locale, n *activities_model.Notification) (*user_model.User, string) {
	title := n.Issue.Title
	c := n.Comment
	if c == nil {
		if n.Issue.LoadPoster(ctx) != nil {
			return nil, ""
		}
		return n.Issue.Poster, l.TrString("asc_shell.bell_opened", title)
	}
	if c.LoadPoster(ctx) != nil {
		return nil, ""
	}
	key := "asc_shell.bell_updated"
	switch c.Type {
	case issues_model.CommentTypeComment, issues_model.CommentTypeCode:
		key = "asc_shell.bell_commented"
	case issues_model.CommentTypeReview:
		key = "asc_shell.bell_reviewed"
		if c.LoadReview(ctx) == nil && c.Review != nil {
			switch c.Review.Type {
			case issues_model.ReviewTypeApprove:
				key = "asc_shell.bell_approved"
			case issues_model.ReviewTypeReject:
				key = "asc_shell.bell_changes"
			}
		}
	case issues_model.CommentTypeClose:
		key = "asc_shell.bell_closed"
	case issues_model.CommentTypeReopen:
		key = "asc_shell.bell_reopened"
	case issues_model.CommentTypeMergePull:
		key = "asc_shell.bell_merged"
	}
	return c.Poster, l.TrString(key, title)
}

// ascBellHub lists the newest notifications of the hub that the person may still read.
func ascBellHub(ctx gocontext.Context, l translation.Locale, doer *user_model.User) []AscBellItem {
	list, err := db.Find[activities_model.Notification](ctx, activities_model.FindNotificationOptions{
		ListOptions: db.ListOptions{Page: 1, PageSize: ascBellMax * 2},
		UserID:      doer.ID,
		Status:      []activities_model.NotificationStatus{activities_model.NotificationStatusUnread, activities_model.NotificationStatusRead, activities_model.NotificationStatusPinned},
	})
	if err != nil {
		log.Error("asc bell: notifications of user %d: %v", doer.ID, err)
		return nil
	}
	var out []AscBellItem
	for _, n := range list {
		if n.LoadAttributes(ctx) != nil || n.Repository == nil {
			continue
		}
		perm, err := access.GetUserRepoPermission(ctx, n.Repository, doer)
		if err != nil {
			continue
		}
		it := AscBellItem{Link: n.Link(ctx), When: n.UpdatedUnix.AsTime(), Unread: n.Status == activities_model.NotificationStatusUnread}
		switch n.Source {
		case activities_model.NotificationSourceIssue, activities_model.NotificationSourcePullRequest:
			if n.Issue == nil || !perm.CanReadIssuesOrPulls(n.Issue.IsPull) {
				continue
			}
			it.Actor, it.Text = ascBellVerb(ctx, l, n)
			it.Sub = n.Repository.FullName() + " #" + strconv.FormatInt(n.Issue.Index, 10)
		case activities_model.NotificationSourceRepository:
			// a transfer offered to the person: they may not read the repository yet
			if n.Repository.LoadOwner(ctx) != nil {
				continue
			}
			it.Actor, it.Text = n.Repository.Owner, l.TrString("asc_shell.bell_transfer", n.Repository.FullName())
			it.Link = n.Repository.Link()
		default:
			continue
		}
		if it.Actor == nil || it.Text == "" {
			continue
		}
		it.Name = it.Actor.GetDisplayName()
		out = append(out, it)
	}
	return out
}

// ascBellInitials is the mark of an event asc reported: the agent of an agent event ("Agent coder-2 finished: ..."
// gives c2), else the first two letters of the name of the event (Checks failed gives ch).
func ascBellInitials(l translation.Locale, event, title string) string {
	name := l.TrString("asc_work.ev_" + event)
	if rest, ok := strings.CutPrefix(title, "Agent "); ok && strings.HasPrefix(event, "agent_") {
		if f := strings.Fields(rest); len(f) > 0 {
			name = strings.TrimRight(f[0], ":")
		}
	}
	r := []rune(strings.ToLower(name))
	switch {
	case len(r) == 0:
		return "?"
	case len(r) > 1 && r[len(r)-1] >= '0' && r[len(r)-1] <= '9':
		return string([]rune{r[0], r[len(r)-1]})
	case len(r) > 1:
		return string(r[:2])
	}
	return string(r)
}

// ascBellEvents lists the newest events asc reported to the person, of repositories they may still read.
func ascBellEvents(ctx gocontext.Context, l translation.Locale, doer *user_model.User) []AscBellItem {
	rows, err := user_model.RecentAscNotifications(ctx, doer.ID, ascBellMax*2)
	if err != nil {
		log.Error("asc bell: events of user %d: %v", doer.ID, err)
		return nil
	}
	var out []AscBellItem
	for _, r := range rows {
		repo, err := repo_model.GetRepositoryByID(ctx, r.RepoID)
		if err != nil {
			continue
		}
		perm, err := access.GetUserRepoPermission(ctx, repo, doer)
		if err != nil || !perm.HasAccess() {
			continue
		}
		sub := r.Detail
		if sub == "" {
			sub = repo.FullName()
		}
		out = append(out, AscBellItem{
			Initials: ascBellInitials(l, r.Event, r.Title),
			Text:     r.Title,
			Sub:      sub,
			Link:     setting.AppSubURL + "/-/asc/bell/open/" + strconv.FormatInt(r.ID, 10),
			When:     r.SentUnix.AsTime(),
			Unread:   r.ReadUnix == 0,
		})
	}
	return out
}

// ascBellUnread is the number of the bell: the unread notifications of the hub and the unread events of asc.
func ascBellUnread(ctx gocontext.Context, userID int64) int64 {
	hub, err := db.Count[activities_model.Notification](ctx, activities_model.FindNotificationOptions{
		UserID: userID,
		Status: []activities_model.NotificationStatus{activities_model.NotificationStatusUnread},
	})
	if err != nil {
		log.Error("asc bell: unread of user %d: %v", userID, err)
	}
	return hub + AscUnreadEvents(ctx, userID)
}

// AscUnreadEvents is the number of events asc reported that the person has not seen; 0 when ASC is off.
func AscUnreadEvents(ctx gocontext.Context, userID int64) int64 {
	if !setting.Asc.Enabled {
		return 0
	}
	n, err := user_model.CountUnreadAscNotifications(ctx, userID)
	if err != nil {
		log.Error("asc bell: unread events of user %d: %v", userID, err)
		return 0
	}
	return n
}

// ascBellMerge puts both lists together, newest first, at most ascBellMax.
func ascBellMerge(hub, events []AscBellItem) []AscBellItem {
	all := append(slices.Clone(hub), events...)
	slices.SortStableFunc(all, func(a, b AscBellItem) int { return b.When.Compare(a.When) })
	if len(all) > ascBellMax {
		all = all[:ascBellMax]
	}
	return all
}

// AscBell answers `GET /-/asc/bell`: the inside of the popover, loaded when it opens.
func AscBell(ctx *context.Context) {
	if !setting.Asc.Enabled {
		ctx.NotFound("AscBell", nil)
		return
	}
	ctx.Data["BellItems"] = ascBellMerge(ascBellHub(ctx, ctx.Locale, ctx.Doer), ascBellEvents(ctx, ctx.Locale, ctx.Doer))
	ctx.Data["BellUnread"] = ascBellUnread(ctx, ctx.Doer.ID)
	ctx.Resp.Header().Set("Cache-Control", "private, no-store")
	ctx.HTML(http.StatusOK, tplAscBell)
}

// AscBellReadPost answers `POST /-/asc/bell/read`: Mark all read, for the notifications of the hub and the events of
// asc.
func AscBellReadPost(ctx *context.Context) {
	if !setting.Asc.Enabled {
		ctx.NotFound("AscBellReadPost", nil)
		return
	}
	if err := activities_model.UpdateNotificationStatuses(ctx, ctx.Doer, activities_model.NotificationStatusUnread, activities_model.NotificationStatusRead); err != nil {
		ctx.ServerError("UpdateNotificationStatuses", err)
		return
	}
	if err := user_model.MarkAscNotificationsRead(ctx, ctx.Doer.ID, 0); err != nil {
		ctx.ServerError("MarkAscNotificationsRead", err)
		return
	}
	ctx.Status(http.StatusNoContent)
}

// AscBellOpen answers `GET /-/asc/bell/open/{id}`: an event of the popover was opened. It is read now, and the person
// goes where it points (a path of this hub, checked when asc reported it). Only the person told may open it.
func AscBellOpen(ctx *context.Context) {
	if !setting.Asc.Enabled {
		ctx.NotFound("AscBellOpen", nil)
		return
	}
	row, err := user_model.GetAscNotification(ctx, ctx.Doer.ID, ctx.ParamsInt64("id"))
	if err != nil {
		ctx.ServerError("GetAscNotification", err)
		return
	}
	if row == nil {
		ctx.NotFound("AscBellOpen", nil)
		return
	}
	if err := user_model.MarkAscNotificationsRead(ctx, ctx.Doer.ID, row.ID); err != nil {
		ctx.ServerError("MarkAscNotificationsRead", err)
		return
	}
	path := row.Path
	if !strings.HasPrefix(path, "/") || strings.HasPrefix(path, "//") {
		path = "/notifications"
	}
	ctx.Redirect(setting.AppSubURL + path)
}
