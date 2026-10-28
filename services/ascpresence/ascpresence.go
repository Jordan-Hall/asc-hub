// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

// Package ascpresence keeps a coarse "last seen" of signed-in people and answers whether the green dot of a profile
// shows. The time is stored at most once every few minutes, and a person who switched "Show when I am online" off
// is never reported as online to anyone else.
package ascpresence

import (
	"context"
	"strconv"
	"sync"
	"time"

	user_model "forgejo.org/models/user"
)

const (
	lastSeenKey = "asc.last_seen"
	showKey     = "asc.show_online"

	// WriteEvery is the least time between two stored updates of one person: the granularity of "last seen".
	WriteEvery = 5 * time.Minute
	// OnlineWithin is how recent the last request must be for the dot to show.
	OnlineWithin = 10 * time.Minute
)

var (
	mu     sync.Mutex
	stored = map[int64]time.Time{} // when this process last stored the time of a person
	now    = time.Now
)

// Touch notes that the person just made a request. It writes to the database at most once per WriteEvery.
func Touch(ctx context.Context, u *user_model.User) {
	if u == nil || u.ID <= 0 {
		return
	}
	t := now()
	mu.Lock()
	last, ok := stored[u.ID]
	if ok && t.Sub(last) < WriteEvery {
		mu.Unlock()
		return
	}
	if len(stored) > 50000 { // bounded: forget everyone, the next request of each person writes again
		stored = map[int64]time.Time{}
	}
	stored[u.ID] = t
	mu.Unlock()
	_ = user_model.SetUserSetting(ctx, u.ID, lastSeenKey, strconv.FormatInt(t.Unix(), 10))
}

// SetShowOnline stores the choice "Show when I am online".
func SetShowOnline(ctx context.Context, userID int64, show bool) error {
	return user_model.SetUserSetting(ctx, userID, showKey, strconv.FormatBool(show))
}

// ShowOnline reports the choice of the person; the default is to show.
func ShowOnline(ctx context.Context, userID int64) bool {
	v, err := user_model.GetUserSetting(ctx, userID, showKey, "true")
	return err != nil || v != "false"
}

// Online reports whether the dot shows for the person: they allow it and made a request lately. The viewer's right to
// see the profile at all is checked by the page that asks.
func Online(ctx context.Context, userID int64) bool {
	if !ShowOnline(ctx, userID) {
		return false
	}
	v, err := user_model.GetUserSetting(ctx, userID, lastSeenKey, "")
	if err != nil || v == "" {
		return false
	}
	sec, err := strconv.ParseInt(v, 10, 64)
	return err == nil && now().Sub(time.Unix(sec, 0)) <= OnlineWithin
}
