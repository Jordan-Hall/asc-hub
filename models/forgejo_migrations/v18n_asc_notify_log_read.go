// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package forgejo_migrations

import "code.forgejo.org/xorm/xorm"

func init() {
	registerMigration(&Migration{
		Description: "add asc_notify_log.detail and read_unix: the notifications popover shows the detail line and the unread events; what was told before is read",
		Upgrade:     addAscNotifyLogRead,
	})
}

func addAscNotifyLogRead(x *xorm.Engine) error {
	type AscNotifyLog struct {
		Detail   string `xorm:"VARCHAR(500) NOT NULL DEFAULT ''"`
		ReadUnix int64  `xorm:"INDEX NOT NULL DEFAULT 0"`
	}
	if _, err := x.SyncWithOptions(xorm.SyncOptions{IgnoreDropIndices: true}, new(AscNotifyLog)); err != nil {
		return err
	}
	// what was told before the popover existed was seen in the inbox, the email or the browser already: do not bring
	// 90 days of it back as unread
	_, err := x.Exec("UPDATE `asc_notify_log` SET `read_unix` = `sent_unix` WHERE `read_unix` = 0")
	return err
}
