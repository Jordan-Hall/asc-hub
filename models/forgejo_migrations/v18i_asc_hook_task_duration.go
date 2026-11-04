// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package forgejo_migrations

import "code.forgejo.org/xorm/xorm"

func init() {
	registerMigration(&Migration{
		Description: "add hook_task.duration_ms: how long the receiver took to answer a webhook delivery",
		Upgrade:     addHookTaskDuration,
	})
}

func addHookTaskDuration(x *xorm.Engine) error {
	type HookTask struct {
		DurationMS int64 `xorm:"NOT NULL DEFAULT 0"`
	}
	_, err := x.SyncWithOptions(xorm.SyncOptions{IgnoreDropIndices: true}, new(HookTask))
	return err
}
