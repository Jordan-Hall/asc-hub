// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package forgejo_migrations

import (
	"code.forgejo.org/xorm/xorm"
)

func init() {
	registerMigration(&Migration{
		Description: "add repo_trash: deleted repositories wait here for 30 days before they are purged",
		Upgrade:     addRepoTrash,
	})
}

func addRepoTrash(x *xorm.Engine) error {
	type RepoTrash struct {
		ID          int64 `xorm:"pk autoincr"`
		RepoID      int64 `xorm:"UNIQUE NOT NULL"`
		OwnerID     int64 `xorm:"INDEX NOT NULL"`
		DeletedByID int64 `xorm:"NOT NULL DEFAULT 0"`
		WasPrivate  bool  `xorm:"NOT NULL DEFAULT false"`
		DeletedUnix int64 `xorm:"NOT NULL"`
		PurgeUnix   int64 `xorm:"INDEX NOT NULL"`
	}
	_, err := x.SyncWithOptions(xorm.SyncOptions{IgnoreDropIndices: true}, new(RepoTrash))
	return err
}
