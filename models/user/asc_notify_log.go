// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package user

import (
	"context"

	"forgejo.org/models/db"
	"forgejo.org/modules/timeutil"
)

// AscNotifyLog records that the hub told a person about an event of a repository (the inbox keeps everything; Emailed
// says whether an email went out as well). It feeds the "Last notified" column of the Notifications page.
type AscNotifyLog struct {
	ID       int64              `xorm:"pk autoincr"`
	UserID   int64              `xorm:"INDEX(user_repo) NOT NULL"`
	RepoID   int64              `xorm:"INDEX(user_repo) NOT NULL"`
	Event    string             `xorm:"VARCHAR(40) NOT NULL"`
	Title    string             `xorm:"VARCHAR(200) NOT NULL DEFAULT ''"`
	Path     string             `xorm:"VARCHAR(300) NOT NULL DEFAULT ''"`
	EventID  string             `xorm:"VARCHAR(64) NOT NULL DEFAULT '' INDEX"` // x-asc-event-id of asc serve: a retry is told once
	Emailed  bool               `xorm:"NOT NULL DEFAULT false"`
	Digested bool               `xorm:"NOT NULL DEFAULT false"` // listed in a daily summary already
	SentUnix timeutil.TimeStamp `xorm:"INDEX NOT NULL"`
}

func init() {
	db.RegisterModel(new(AscNotifyLog))
}

// TableName is the table of the model.
func (AscNotifyLog) TableName() string { return "asc_notify_log" }

// RecordAscNotification stores one notification.
func RecordAscNotification(ctx context.Context, userID, repoID int64, event, title, path, eventID string, emailed bool) error {
	_, err := db.GetEngine(ctx).Insert(&AscNotifyLog{UserID: userID, RepoID: repoID, Event: event, Title: title, Path: path, EventID: eventID, Emailed: emailed, SentUnix: timeutil.TimeStampNow()})
	return err
}

// PendingAscDigest returns the entries of the person that no email and no summary has carried yet, oldest first, newer
// than since.
func PendingAscDigest(ctx context.Context, userID int64, since timeutil.TimeStamp) ([]*AscNotifyLog, error) {
	var rows []*AscNotifyLog
	err := db.GetEngine(ctx).Where("user_id = ? AND emailed = ? AND digested = ? AND sent_unix > ?", userID, false, false, since).
		OrderBy("sent_unix, id").Limit(500).Find(&rows)
	return rows, err
}

// MarkAscDigested marks entries as carried by a summary.
func MarkAscDigested(ctx context.Context, userID int64, ids []int64) error {
	if len(ids) == 0 {
		return nil
	}
	_, err := db.GetEngine(ctx).Table("asc_notify_log").Where("user_id = ?", userID).In("id", ids).Cols("digested").Update(map[string]any{"digested": true})
	return err
}

// LastAscNotified returns, for each of the repositories, when the person was last told about it.
func LastAscNotified(ctx context.Context, userID int64, repoIDs []int64) (map[int64]timeutil.TimeStamp, error) {
	out := map[int64]timeutil.TimeStamp{}
	if len(repoIDs) == 0 {
		return out, nil
	}
	var rows []struct {
		RepoID int64
		Last   int64
	}
	err := db.GetEngine(ctx).Table("asc_notify_log").Select("repo_id, max(sent_unix) AS last").
		Where("user_id = ?", userID).In("repo_id", repoIDs).GroupBy("repo_id").Find(&rows)
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		out[r.RepoID] = timeutil.TimeStamp(r.Last)
	}
	return out, nil
}

// DeleteOldAscNotifyLog removes entries older than 90 days.
func DeleteOldAscNotifyLog(ctx context.Context) (int64, error) {
	return db.GetEngine(ctx).Where("sent_unix < ?", timeutil.TimeStampNow().Add(-90*86400)).Delete(new(AscNotifyLog))
}

// HasAscNotificationEvent reports whether the person was already told about the event with this id (the id asc serve
// puts in x-asc-event-id, the same for every retry of one event).
func HasAscNotificationEvent(ctx context.Context, userID int64, eventID string) (bool, error) {
	if eventID == "" {
		return false, nil
	}
	return db.GetEngine(ctx).Where("user_id = ? AND event_id = ?", userID, eventID).Exist(new(AscNotifyLog))
}
