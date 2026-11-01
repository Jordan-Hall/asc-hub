// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package auth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"time"

	"forgejo.org/models/db"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/timeutil"
)

// browserSessionTouchEvery is how long a session's last activity may lag behind: a request writes it at most this often.
const browserSessionTouchEvery = 5 * time.Minute

// BrowserSession is one browser a person is signed in on. The session ID itself is never kept, only its hash; the row
// is what lets the person see their signed-in browsers and sign one out from another. A revoked row stays until its
// session would have expired, so the browser cannot come back with the same cookie.
type BrowserSession struct {
	ID             int64              `xorm:"pk autoincr"`
	UID            int64              `xorm:"INDEX NOT NULL"`
	SIDHash        string             `xorm:"VARCHAR(64) UNIQUE NOT NULL"`
	RememberKey    string             `xorm:"VARCHAR(255) NOT NULL DEFAULT ''"`
	UserAgent      string             `xorm:"VARCHAR(255) NOT NULL DEFAULT ''"`
	IP             string             `xorm:"VARCHAR(64) NOT NULL DEFAULT ''"`
	CreatedUnix    timeutil.TimeStamp `xorm:"NOT NULL DEFAULT 0"`
	LastActiveUnix timeutil.TimeStamp `xorm:"INDEX NOT NULL DEFAULT 0"`
	RevokedUnix    timeutil.TimeStamp `xorm:"NOT NULL DEFAULT 0"`
}

func init() {
	db.RegisterModel(new(BrowserSession))
}

// HashSessionID is the form a session ID is kept and looked up in.
func HashSessionID(sid string) string {
	h := sha256.Sum256([]byte(sid))
	return hex.EncodeToString(h[:])
}

// browserSessionLifetime is how long a row may be idle before it is dropped: a session's own lifetime, or the
// remember-me lifetime when the browser can sign itself back in.
func browserSessionLifetime(remember bool) time.Duration {
	life := time.Duration(setting.SessionConfig.Maxlifetime) * time.Second
	if remember {
		if r := time.Duration(setting.LogInRememberDays) * 24 * time.Hour; r > life {
			life = r
		}
	}
	return life
}

// TouchBrowserSession records that the session sid of person uid made a request, and says whether that session was
// signed out from elsewhere (the caller then ends it). The first request of a session adds its row.
func TouchBrowserSession(ctx context.Context, uid int64, sid, rememberKey, userAgent, ip string) (revoked bool, err error) {
	if sid == "" {
		return false, nil
	}
	hash := HashSessionID(sid)
	now := timeutil.TimeStampNow()
	row := &BrowserSession{SIDHash: hash}
	has, err := db.GetEngine(ctx).Get(row)
	if err != nil {
		return false, err
	}
	if !has {
		_, err = db.GetEngine(ctx).Insert(&BrowserSession{
			UID: uid, SIDHash: hash, RememberKey: rememberKey, UserAgent: clipSessionField(userAgent, 255), IP: clipSessionField(ip, 64),
			CreatedUnix: now, LastActiveUnix: now,
		})
		if err != nil {
			// two first requests of one session at once: the other one added the row
			if again, gerr := db.GetEngine(ctx).Exist(&BrowserSession{SIDHash: hash}); gerr == nil && again {
				return false, nil
			}
		}
		return false, err
	}
	if row.RevokedUnix != 0 || row.UID != uid {
		return true, nil
	}
	if now.AsTime().Sub(row.LastActiveUnix.AsTime()) < browserSessionTouchEvery && (rememberKey == "" || rememberKey == row.RememberKey) {
		return false, nil
	}
	row.LastActiveUnix, row.IP = now, clipSessionField(ip, 64)
	cols := []string{"last_active_unix", "ip"}
	if rememberKey != "" {
		row.RememberKey = rememberKey
		cols = append(cols, "remember_key")
	}
	_, err = db.GetEngine(ctx).ID(row.ID).Cols(cols...).Update(row)
	return false, err
}

// ListBrowserSessions returns the browsers person uid is signed in on, the most recently active first. Rows that
// expired are dropped on the way.
func ListBrowserSessions(ctx context.Context, uid int64) ([]*BrowserSession, error) {
	if err := gcBrowserSessions(ctx, uid); err != nil {
		return nil, err
	}
	var rows []*BrowserSession
	err := db.GetEngine(ctx).Where("uid = ? AND revoked_unix = 0", uid).OrderBy("last_active_unix DESC, id DESC").Find(&rows)
	return rows, err
}

func gcBrowserSessions(ctx context.Context, uid int64) error {
	now := time.Now()
	short := timeutil.TimeStamp(now.Add(-browserSessionLifetime(false)).Unix())
	long := timeutil.TimeStamp(now.Add(-browserSessionLifetime(true)).Unix())
	_, err := db.GetEngine(ctx).Where("uid = ? AND last_active_unix < ? AND (remember_key = '' OR revoked_unix <> 0 OR last_active_unix < ?)", uid, short, long).
		Delete(new(BrowserSession))
	return err
}

// RevokeBrowserSession signs session id of person uid out: its next request ends it, and its remember-me cookie stops
// working. It says whether there was such a session.
func RevokeBrowserSession(ctx context.Context, uid, id int64) (bool, error) {
	var found bool
	err := db.WithTx(ctx, func(ctx context.Context) error {
		row := &BrowserSession{}
		has, err := db.GetEngine(ctx).Where("id = ? AND uid = ? AND revoked_unix = 0", id, uid).Get(row)
		if err != nil || !has {
			return err
		}
		found = true
		return revokeBrowserSession(ctx, row)
	})
	return found, err
}

// RevokeOtherBrowserSessions signs person uid out of every browser but the one with session keepSID. It returns how
// many were signed out.
func RevokeOtherBrowserSessions(ctx context.Context, uid int64, keepSID string) (int, error) {
	keep := HashSessionID(keepSID)
	n := 0
	err := db.WithTx(ctx, func(ctx context.Context) error {
		var rows []*BrowserSession
		if err := db.GetEngine(ctx).Where("uid = ? AND revoked_unix = 0 AND sid_hash <> ?", uid, keep).Find(&rows); err != nil {
			return err
		}
		for _, row := range rows {
			if err := revokeBrowserSession(ctx, row); err != nil {
				return err
			}
		}
		n = len(rows)
		return nil
	})
	return n, err
}

func revokeBrowserSession(ctx context.Context, row *BrowserSession) error {
	if row.RememberKey != "" {
		if _, err := db.GetEngine(ctx).Where("uid = ? AND lookup_key = ?", row.UID, row.RememberKey).Delete(new(AuthorizationToken)); err != nil {
			return err
		}
	}
	row.RevokedUnix = timeutil.TimeStampNow()
	_, err := db.GetEngine(ctx).ID(row.ID).Cols("revoked_unix").Update(row)
	return err
}

// EndBrowserSession drops the row of session sid: the person signed out of it themselves.
func EndBrowserSession(ctx context.Context, sid string) error {
	if sid == "" {
		return nil
	}
	_, err := db.GetEngine(ctx).Where("sid_hash = ?", HashSessionID(sid)).Delete(new(BrowserSession))
	return err
}

func clipSessionField(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
