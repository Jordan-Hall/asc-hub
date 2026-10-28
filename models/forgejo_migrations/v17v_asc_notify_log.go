// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package forgejo_migrations

import (
	"code.forgejo.org/xorm/xorm"
)

func init() {
	registerMigration(&Migration{
		Description: "add asc_notify_log: when the hub told a person about an event of a repository",
		Upgrade:     addAscNotifyLog,
	})
}

func addAscNotifyLog(x *xorm.Engine) error {
	type AscNotifyLog struct {
		ID       int64  `xorm:"pk autoincr"`
		UserID   int64  `xorm:"INDEX(user_repo) NOT NULL"`
		RepoID   int64  `xorm:"INDEX(user_repo) NOT NULL"`
		Event    string `xorm:"VARCHAR(40) NOT NULL"`
		Title    string `xorm:"VARCHAR(200) NOT NULL DEFAULT ''"`
		Path     string `xorm:"VARCHAR(300) NOT NULL DEFAULT ''"`
		EventID  string `xorm:"VARCHAR(64) NOT NULL DEFAULT '' INDEX"`
		Emailed  bool   `xorm:"NOT NULL DEFAULT false"`
		Digested bool   `xorm:"NOT NULL DEFAULT false"`
		SentUnix int64  `xorm:"INDEX NOT NULL"`
	}
	_, err := x.SyncWithOptions(xorm.SyncOptions{IgnoreDropIndices: true}, new(AscNotifyLog))
	return err
}
