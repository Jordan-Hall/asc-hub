// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

// Package ascnotify holds what a person chose on the Notifications settings page: which events reach them by email,
// and the quiet hours in their time zone. The mailer asks it before it sends an email, so the page is not a form
// that saves into the void.
package ascnotify

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"time"

	"forgejo.org/models/db"
	user_model "forgejo.org/models/user"
)

// The events of the Notifications page that the hub sends email for.
const (
	EventReviewRequested = "review_requested"
	EventMentioned       = "mentioned"
)

// Events lists every event the page shows, in the order of the board. The inbox keeps all of them; only the ones in
// EmailEvents can be switched for email, because the hub sends no email for the others yet.
var (
	Events = []string{"review_requested", "mentioned", "agent_question", "agent_finished", "checks_failed", "restack_conflict", "merge_queue", "policy_changed"}
	// EmailEvents are the events a person can switch for email: the hub sends mail for every one of them, review requests
	// and mentions from its own mailer for the pull requests of the hub, and every event when asc serve reports it (IngestEvents:
	// reviews and mentions made through asc, agent runs, checks, restacks, the merge queue, policy).
	EmailEvents  = Events
	IngestEvents = Events
)

const settingKey = "asc.notify"

// Prefs is the stored choice. The zero value means the defaults: every email event on, no quiet hours.
type Prefs struct {
	// EmailOff lists the email events the person switched off.
	EmailOff []string `json:"email_off,omitempty"`
	// PushOff lists the events the person switched off for notifications in the browser (Web Push).
	PushOff []string `json:"push_off,omitempty"`
	// Digest sends the events that are not emailed one by one as one summary a day, at 08:00 in the time zone TZ.
	Digest bool `json:"digest,omitempty"`
	// Quiet pauses email between From and Until (HH:MM, 24 h) in the time zone TZ.
	Quiet QuietHours `json:"quiet"`
	// TZ is an IANA time zone name; empty means UTC.
	TZ string `json:"tz,omitempty"`
}

// QuietHours is the overnight pause of email.
type QuietHours struct {
	On    bool   `json:"on"`
	From  string `json:"from,omitempty"`
	Until string `json:"until,omitempty"`
}

// The quiet hours of the board, used when a person turns them on.
const (
	DefaultFrom  = "20:00"
	DefaultUntil = "07:30"
)

// Load reads the choice of a person; a missing or unreadable value means the defaults.
func Load(ctx context.Context, userID int64) Prefs {
	raw, err := user_model.GetUserSetting(ctx, userID, settingKey, "")
	var p Prefs
	if err != nil || raw == "" || json.Unmarshal([]byte(raw), &p) != nil {
		return Prefs{}
	}
	return p
}

// TimeZoneName is the IANA time zone the person chose on the profile page, or "" when they chose none or the name is not
// one this hub can load. It is for drawing times in their zone, so it never fails.
func TimeZoneName(ctx context.Context, userID int64) string {
	tz := Load(ctx, userID).TZ
	if tz == "" {
		return ""
	}
	if _, err := time.LoadLocation(tz); err != nil {
		return ""
	}
	return tz
}

// Save validates and stores the choice.
func Save(ctx context.Context, userID int64, p Prefs) error {
	if err := p.Validate(); err != nil {
		return err
	}
	raw, err := json.Marshal(p)
	if err != nil {
		return err
	}
	return user_model.SetUserSetting(ctx, userID, settingKey, string(raw))
}

// Validate refuses what the page cannot have sent.
func (p Prefs) Validate() error {
	for _, e := range p.EmailOff {
		if !slices.Contains(EmailEvents, e) {
			return fmt.Errorf("%q is not an event with email", e)
		}
	}
	for _, e := range p.PushOff {
		if !slices.Contains(Events, e) {
			return fmt.Errorf("%q is not an event with notifications", e)
		}
	}
	if p.TZ != "" {
		if _, err := time.LoadLocation(p.TZ); err != nil {
			return fmt.Errorf("unknown time zone %q", p.TZ)
		}
	}
	if p.Quiet.On {
		if _, err := minutes(p.Quiet.From); err != nil {
			return err
		}
		if _, err := minutes(p.Quiet.Until); err != nil {
			return err
		}
	}
	return nil
}

// PushOn reports whether the person wants a browser notification for the event.
func (p Prefs) PushOn(event string) bool { return !slices.Contains(p.PushOff, event) }

// EmailOn reports whether the person wants email for the event.
func (p Prefs) EmailOn(event string) bool { return !slices.Contains(p.EmailOff, event) }

func minutes(hhmm string) (int, error) {
	t, err := time.Parse("15:04", hhmm)
	if err != nil {
		return 0, fmt.Errorf("%q is not a time of day like 20:00", hhmm)
	}
	return t.Hour()*60 + t.Minute(), nil
}

// InQuietHours reports whether email is paused at the moment now: inside the window From to Until in the time zone of
// the person, a window that may cross midnight.
func (p Prefs) InQuietHours(now time.Time) bool {
	if !p.Quiet.On {
		return false
	}
	from, err1 := minutes(p.Quiet.From)
	until, err2 := minutes(p.Quiet.Until)
	if err1 != nil || err2 != nil || from == until {
		return false
	}
	loc := time.UTC
	if p.TZ != "" {
		if l, err := time.LoadLocation(p.TZ); err == nil {
			loc = l
		}
	}
	local := now.In(loc)
	at := local.Hour()*60 + local.Minute()
	if from < until {
		return at >= from && at < until
	}
	return at >= from || at < until
}

// EmailAllowed is the question of the mailer: may this person get an email for the event now? A mail that is not one
// of the events of the page (event is empty) is only held back by quiet hours.
func EmailAllowed(ctx context.Context, userID int64, event string, now time.Time) bool {
	p := Load(ctx, userID)
	if p.InQuietHours(now) {
		return false
	}
	return event == "" || p.EmailOn(event)
}

// IsIngestEvent reports whether asc serve may report the event to the hub.
func IsIngestEvent(event string) bool { return slices.Contains(IngestEvents, event) }

// PushAllowed is the question of the sender of browser notifications: may this person be notified of the event now?
// Quiet hours pause notifications in the browser as they pause email.
func PushAllowed(ctx context.Context, userID int64, event string, now time.Time) bool {
	p := Load(ctx, userID)
	return !p.InQuietHours(now) && p.PushOn(event)
}

// DigestHour is the hour of the day, in the time zone of the person, when the daily summary is sent.
const DigestHour = 8

// DigestDue reports whether the summary of the person is due at now: they asked for it and it is DigestHour o'clock
// in their time zone.
func (p Prefs) DigestDue(now time.Time) bool {
	if !p.Digest {
		return false
	}
	loc := time.UTC
	if p.TZ != "" {
		if l, err := time.LoadLocation(p.TZ); err == nil {
			loc = l
		}
	}
	return now.In(loc).Hour() == DigestHour
}

// DigestUserIDs lists the people who asked for the daily summary.
func DigestUserIDs(ctx context.Context) ([]int64, error) {
	var ids []int64
	err := db.GetEngine(ctx).Table("user_setting").
		Where("setting_key = ? AND setting_value LIKE ?", settingKey, `%"digest":true%`).Cols("user_id").Find(&ids)
	return ids, err
}
