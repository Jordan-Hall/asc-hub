// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

// Package ascexportmail emails a person the link to an export of their data once asc serve has made it: the account page
// remembers the export that was asked for, and a cron task asks asc serve for its state until it is ready or failed.
package ascexportmail

import (
	"context"
	"errors"
	"strings"
	"time"

	user_model "forgejo.org/models/user"
	"forgejo.org/modules/log"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/translation"
	"forgejo.org/services/asctrust"
	"forgejo.org/services/mailer"
)

const (
	batch  = 200
	giveUp = 48 * time.Hour // an export that is not over two days after it was asked for is not watched any more
)

// Send is the function that sends the email; tests replace it.
var Send = func(to, subject, body string) { mailer.SendAsync(mailer.NewMessage(to, subject, body)) }

// GetExport asks asc serve, as the person, for the state of one of their exports; tests replace it.
var GetExport = func(ctx context.Context, u *user_model.User, id string) (*asctrust.ExportRow, error) {
	conn, err := asctrust.Conn()
	if err != nil {
		return nil, err
	}
	cctx, release, err := asctrust.UserCall(ctx, u, asctrust.CallAccount, nil)
	if err != nil {
		return nil, err
	}
	defer release()
	return asctrust.GetExport(cctx, conn, id)
}

// Enabled says whether the hub emails about exports: it needs a mail service.
func Enabled() bool { return setting.MailService != nil }

// Watch remembers an export the person asked for, so they get an email when it is ready.
func Watch(ctx context.Context, u *user_model.User, exportID string) {
	if !Enabled() || u == nil {
		return
	}
	if err := user_model.WatchAscExport(ctx, u.ID, exportID); err != nil {
		log.Error("asc export mail: watch %s of user %d: %v", exportID, u.ID, err)
	}
}

// Run looks at the watched exports and emails the people whose export is ready or failed. It returns how many emails
// it sent. Without a mail service it does nothing.
func Run(ctx context.Context, now time.Time) (int, error) {
	if !Enabled() {
		return 0, nil
	}
	watches, err := user_model.AscExportWatches(ctx, batch)
	if err != nil {
		return 0, err
	}
	sent := 0
	for _, w := range watches {
		done, mailed := check(ctx, w, now)
		if mailed {
			sent++
		}
		if done {
			if err := user_model.UnwatchAscExport(ctx, w.ID); err != nil {
				return sent, err
			}
		}
	}
	return sent, nil
}

// check looks at one export: done says the watch is over, mailed that an email went out.
func check(ctx context.Context, w *user_model.AscExportWatch, now time.Time) (done, mailed bool) {
	stale := now.Sub(w.CreatedUnix.AsTime()) > giveUp
	u, err := user_model.GetUserByID(ctx, w.UserID)
	if err != nil {
		if user_model.IsErrUserNotExist(err) {
			return true, false
		}
		log.Error("asc export mail: user %d: %v", w.UserID, err)
		return stale, false
	}
	if !u.IsActive || u.ProhibitLogin || u.Email == "" {
		return true, false
	}
	row, err := GetExport(ctx, u, w.ExportID)
	if err != nil {
		if errors.Is(err, asctrust.ErrNoDoer) || errors.Is(err, asctrust.ErrForbidden) || errors.Is(err, asctrust.ErrBadInput) {
			return true, false
		}
		if e := asctrust.Classify(err); e.Kind == asctrust.KindNotFound || e.Kind == asctrust.KindDenied {
			return true, false // deleted, or no longer theirs to read
		}
		log.Warn("asc export mail: export %s of user %d: %v", w.ExportID, w.UserID, err)
		return stale, false
	}
	switch {
	case row.Waiting():
		return stale, false
	case row.State == "ready" && row.Downloadable(now):
		subject, text := body(u, row, true)
		Send(u.EmailTo(), subject, text)
		return true, true
	case row.State == "failed":
		subject, text := body(u, row, false)
		Send(u.EmailTo(), subject, text)
		return true, true
	}
	return true, false // expired before it could be told
}

func body(u *user_model.User, row *asctrust.ExportRow, ready bool) (string, string) {
	loc := translation.NewLocale(u.Language)
	page := strings.TrimSuffix(setting.AppURL, "/") + "/user/settings/asc/export"
	var b strings.Builder
	if !ready {
		b.WriteString(loc.TrString("asc_shell.export_mail_failed_lead") + "\n\n")
		if row.Error != "" {
			b.WriteString(row.Error + "\n\n")
		}
		b.WriteString(loc.TrString("asc_shell.export_mail_failed_again") + "\n" + page + "\n")
		return loc.TrString("asc_shell.export_mail_failed_subject"), b.String()
	}
	b.WriteString(loc.TrString("asc_shell.export_mail_ready_lead") + "\n\n")
	b.WriteString(page + "/" + row.ID + "\n\n")
	if !row.Expires.IsZero() {
		b.WriteString(loc.TrString("asc_shell.export_mail_ready_until", row.Expires.UTC().Format("2 Jan 2006 15:04 MST")) + "\n")
	}
	b.WriteString(loc.TrString("asc_shell.export_mail_ready_signin") + "\n")
	return loc.TrString("asc_shell.export_mail_ready_subject"), b.String()
}
