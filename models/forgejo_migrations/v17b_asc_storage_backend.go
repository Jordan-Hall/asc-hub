// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package forgejo_migrations

import (
	"code.forgejo.org/xorm/xorm"
)

func init() {
	registerMigration(&Migration{
		Description: "add StorageBackend to Repository (git, dual or asc)",
		Upgrade:     addRepositoryStorageBackend,
	})
}

func addRepositoryStorageBackend(x *xorm.Engine) error {
	type Repository struct {
		StorageBackend string `xorm:"VARCHAR(8) NOT NULL DEFAULT 'git'"`
	}
	if _, err := x.SyncWithOptions(xorm.SyncOptions{IgnoreDropIndices: true}, new(Repository)); err != nil {
		return err
	}
	// Every existing repository is a git repository. The column default already says so; this
	// also covers a database that added the column without filling it.
	_, err := x.Exec("UPDATE `repository` SET `storage_backend` = 'git' WHERE `storage_backend` IS NULL OR `storage_backend` = ''")
	return err
}
