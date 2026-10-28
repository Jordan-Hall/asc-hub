// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package forgejo_migrations

import (
	"code.forgejo.org/xorm/xorm"
)

func init() {
	registerMigration(&Migration{
		Description: "add asc_profile_pin: the repositories a person pinned to their profile",
		Upgrade:     addAscProfilePin,
	})
}

func addAscProfilePin(x *xorm.Engine) error {
	type AscProfilePin struct {
		ID       int64 `xorm:"pk autoincr"`
		UserID   int64 `xorm:"UNIQUE(user_repo) INDEX NOT NULL"`
		RepoID   int64 `xorm:"UNIQUE(user_repo) NOT NULL"`
		Position int   `xorm:"NOT NULL DEFAULT 0"`
	}
	_, err := x.SyncWithOptions(xorm.SyncOptions{IgnoreDropIndices: true}, new(AscProfilePin))
	return err
}
