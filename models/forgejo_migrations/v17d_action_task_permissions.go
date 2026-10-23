// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package forgejo_migrations

import (
	"code.forgejo.org/xorm/xorm"
)

func init() {
	registerMigration(&Migration{
		Description: "add Permissions to ActionTask",
		Upgrade:     addActionTaskPermissions,
	})
}

func addActionTaskPermissions(x *xorm.Engine) error {
	type ActionTask struct {
		Permissions string `xorm:"TEXT"`
	}
	// Existing tasks have no stored permissions and keep the default capability.
	_, err := x.SyncWithOptions(xorm.SyncOptions{IgnoreDropIndices: true}, new(ActionTask))
	return err
}
