// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package forgejo_migrations

import (
	"code.forgejo.org/xorm/xorm"
)

func init() {
	registerMigration(&Migration{
		Description: "add hidden to project_board: a column of a project board can be hidden and shown again",
		Upgrade:     addProjectColumnHidden,
	})
}

func addProjectColumnHidden(x *xorm.Engine) error {
	type ProjectBoard struct {
		ID     int64
		Hidden bool `xorm:"NOT NULL DEFAULT false"`
	}
	_, err := x.SyncWithOptions(xorm.SyncOptions{IgnoreDropIndices: true}, new(ProjectBoard))
	return err
}
