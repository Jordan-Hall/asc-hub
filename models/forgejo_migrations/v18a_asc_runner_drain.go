// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package forgejo_migrations

import (
	"code.forgejo.org/xorm/xorm"
)

func init() {
	registerMigration(&Migration{
		Description: "add draining to action_runner: a draining runner finishes its job and is given no new one",
		Upgrade:     addRunnerDraining,
	})
}

func addRunnerDraining(x *xorm.Engine) error {
	type ActionRunner struct {
		ID       int64
		Draining bool `xorm:"NOT NULL DEFAULT false"`
	}
	_, err := x.SyncWithOptions(xorm.SyncOptions{IgnoreDropIndices: true}, new(ActionRunner))
	return err
}
