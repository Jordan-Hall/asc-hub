// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package forgejo_migrations

import (
	"code.forgejo.org/xorm/xorm"
)

func init() {
	registerMigration(&Migration{
		Description: "add asc_inbox_state: a user's pin and snooze on a pull request",
		Upgrade:     addAscInboxState,
	})
}

func addAscInboxState(x *xorm.Engine) error {
	type AscInboxState struct {
		ID           int64 `xorm:"pk autoincr"`
		UserID       int64 `xorm:"UNIQUE(user_pull) NOT NULL"`
		PullID       int64 `xorm:"UNIQUE(user_pull) INDEX NOT NULL"`
		Pinned       bool  `xorm:"NOT NULL DEFAULT false"`
		SnoozedUntil int64 `xorm:"NOT NULL DEFAULT 0"`
		UpdatedUnix  int64 `xorm:"updated"`
	}
	_, err := x.SyncWithOptions(xorm.SyncOptions{IgnoreDropIndices: true}, new(AscInboxState))
	return err
}
