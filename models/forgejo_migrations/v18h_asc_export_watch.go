// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package forgejo_migrations

import (
	"forgejo.org/modules/timeutil"

	"code.forgejo.org/xorm/xorm"
)

func init() {
	registerMigration(&Migration{
		Description: "add asc_export_watch: the exports of a person's data the hub emails about when they are ready",
		Upgrade:     addAscExportWatch,
	})
}

func addAscExportWatch(x *xorm.Engine) error {
	type AscExportWatch struct {
		ID          int64              `xorm:"pk autoincr"`
		UserID      int64              `xorm:"UNIQUE(user_export) NOT NULL"`
		ExportID    string             `xorm:"VARCHAR(40) UNIQUE(user_export) NOT NULL"`
		CreatedUnix timeutil.TimeStamp `xorm:"INDEX NOT NULL"`
	}
	_, err := x.SyncWithOptions(xorm.SyncOptions{IgnoreDropIndices: true}, new(AscExportWatch))
	return err
}
