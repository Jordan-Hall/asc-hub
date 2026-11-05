// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package forgejo_migrations

import "code.forgejo.org/xorm/xorm"

func init() {
	registerMigration(&Migration{
		Description: "add asc_inbox_state.done_unix: a person marked a pull request of the inbox done",
		Upgrade:     addAscInboxDone,
	})
}

func addAscInboxDone(x *xorm.Engine) error {
	type AscInboxState struct {
		DoneUnix int64 `xorm:"NOT NULL DEFAULT 0"`
	}
	_, err := x.SyncWithOptions(xorm.SyncOptions{IgnoreDropIndices: true}, new(AscInboxState))
	return err
}
