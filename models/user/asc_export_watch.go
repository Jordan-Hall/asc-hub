// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package user

import (
	"context"

	"forgejo.org/models/db"
	"forgejo.org/modules/timeutil"
)

// AscExportWatch is an export of a person's data that asc serve is still making: the hub asks for its state until it is
// ready or failed, then emails the person and forgets it.
type AscExportWatch struct {
	ID          int64              `xorm:"pk autoincr"`
	UserID      int64              `xorm:"UNIQUE(user_export) NOT NULL"`
	ExportID    string             `xorm:"VARCHAR(40) UNIQUE(user_export) NOT NULL"`
	CreatedUnix timeutil.TimeStamp `xorm:"INDEX NOT NULL"`
}

func init() {
	db.RegisterModel(new(AscExportWatch))
}

// TableName is the table of the model.
func (AscExportWatch) TableName() string { return "asc_export_watch" }

// WatchAscExport remembers an export the person asked for. Asking again for the same export changes nothing.
func WatchAscExport(ctx context.Context, userID int64, exportID string) error {
	has, err := db.GetEngine(ctx).Exist(&AscExportWatch{UserID: userID, ExportID: exportID})
	if err != nil || has {
		return err
	}
	_, err = db.GetEngine(ctx).Insert(&AscExportWatch{UserID: userID, ExportID: exportID, CreatedUnix: timeutil.TimeStampNow()})
	return err
}

// AscExportWatches returns the exports that are watched, oldest first.
func AscExportWatches(ctx context.Context, limit int) ([]*AscExportWatch, error) {
	var rows []*AscExportWatch
	err := db.GetEngine(ctx).OrderBy("created_unix, id").Limit(limit).Find(&rows)
	return rows, err
}

// UnwatchAscExport forgets one watched export.
func UnwatchAscExport(ctx context.Context, id int64) error {
	_, err := db.GetEngine(ctx).ID(id).Delete(new(AscExportWatch))
	return err
}
