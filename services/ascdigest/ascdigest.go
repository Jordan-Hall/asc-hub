// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

// Package ascdigest sends the daily summary: for a person who asked for it on the Notifications page, one email at
// 08:00 in their time zone with what the hub told them since the day before, instead of one email for each event.
package ascdigest

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	access_model "forgejo.org/models/perm/access"
	repo_model "forgejo.org/models/repo"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/log"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/timeutil"
	"forgejo.org/modules/translation"
	"forgejo.org/services/ascnotify"
	"forgejo.org/services/mailer"
)

const (
	lookBack = 36 * time.Hour // a summary missed once is still sent the next day
	maxItems = 60
)

// Send is the function that sends the email; tests replace it.
var Send = func(to, subject, body string) { mailer.SendAsync(mailer.NewMessage(to, subject, body)) }

// Run sends the summaries that are due at now and returns how many it sent. Without a mail service it does nothing,
// and keeps the entries for later.
func Run(ctx context.Context, now time.Time) (int, error) {
	if setting.MailService == nil {
		return 0, nil
	}
	ids, err := ascnotify.DigestUserIDs(ctx)
	if err != nil {
		return 0, err
	}
	sent := 0
	for _, id := range ids {
		prefs := ascnotify.Load(ctx, id)
		if !prefs.DigestDue(now) {
			continue
		}
		u, err := user_model.GetUserByID(ctx, id)
		if err != nil || !u.IsActive || u.EmailNotificationsPreference == user_model.EmailNotificationsDisabled {
			continue
		}
		ok, err := sendOne(ctx, u, prefs, now)
		if err != nil {
			log.Error("asc digest: user %d: %v", id, err)
			continue
		}
		if ok {
			sent++
		}
	}
	return sent, nil
}

func sendOne(ctx context.Context, u *user_model.User, prefs ascnotify.Prefs, now time.Time) (bool, error) {
	rows, err := user_model.PendingAscDigest(ctx, u.ID, timeutil.TimeStamp(now.Add(-lookBack).Unix()))
	if err != nil || len(rows) == 0 {
		return false, err
	}
	loc := translation.NewLocale(u.Language)
	byRepo := map[int64][]string{}
	names := map[int64]string{}
	items := 0
	ids := make([]int64, 0, len(rows))
	base := strings.TrimSuffix(setting.AppURL, "/")
	for _, r := range rows {
		ids = append(ids, r.ID)
		if !prefs.EmailOn(r.Event) || items >= maxItems {
			continue
		}
		repo, err := repo_model.GetRepositoryByID(ctx, r.RepoID)
		if err != nil {
			continue
		}
		perm, err := access_model.GetUserRepoPermission(ctx, repo, u)
		if err != nil || !perm.HasAccess() {
			continue // the access may have ended since: the title of a repository they cannot read is not sent
		}
		names[repo.ID] = repo.FullName()
		link := repo.HTMLURL()
		if r.Path != "" {
			link = base + r.Path
		}
		byRepo[repo.ID] = append(byRepo[repo.ID], fmt.Sprintf("- %s: %s\n  %s", loc.TrString("asc_work.ev_"+r.Event), r.Title, link))
		items++
	}
	if err := user_model.MarkAscDigested(ctx, u.ID, ids); err != nil {
		return false, err
	}
	if items == 0 {
		return false, nil
	}
	repoIDs := make([]int64, 0, len(byRepo))
	for id := range byRepo {
		repoIDs = append(repoIDs, id)
	}
	sort.Slice(repoIDs, func(i, j int) bool { return names[repoIDs[i]] < names[repoIDs[j]] })
	var b strings.Builder
	b.WriteString(loc.TrString("asc_work.dg_lead") + "\n")
	for _, id := range repoIDs {
		b.WriteString("\n" + names[id] + "\n" + strings.Join(byRepo[id], "\n") + "\n")
	}
	b.WriteString("\n" + loc.TrString("asc_work.ev_mail_footer") + " " + base + "/user/settings/notifications\n")
	Send(u.EmailTo(), loc.TrString("asc_work.dg_subject", items), b.String())
	return true, nil
}
