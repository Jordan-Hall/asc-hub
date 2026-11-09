// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package forgejo_migrations

import (
	"code.forgejo.org/xorm/xorm"
)

func init() {
	registerMigration(&Migration{
		Description: "add asc_landed to project_board: the Landed column of a project, where a card goes when its stack lands",
		Upgrade:     addProjectColumnLanded,
	})
}

func addProjectColumnLanded(x *xorm.Engine) error {
	type ProjectBoard struct {
		ID        int64
		AscLanded bool `xorm:"'asc_landed' NOT NULL DEFAULT false"`
	}
	_, err := x.SyncWithOptions(xorm.SyncOptions{IgnoreDropIndices: true}, new(ProjectBoard))
	return err
}
