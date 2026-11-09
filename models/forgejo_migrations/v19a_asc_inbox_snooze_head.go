// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package forgejo_migrations

import (
	"code.forgejo.org/xorm/xorm"
)

func init() {
	registerMigration(&Migration{
		Description: "add asc_inbox_state.snooze_head: a snooze of the inbox that ends when the checks finish",
		Upgrade:     addAscInboxSnoozeHead,
	})
}

func addAscInboxSnoozeHead(x *xorm.Engine) error {
	type AscInboxState struct {
		SnoozeHead string `xorm:"VARCHAR(64) NOT NULL DEFAULT ''"`
	}
	_, err := x.SyncWithOptions(xorm.SyncOptions{IgnoreDropIndices: true}, new(AscInboxState))
	return err
}
