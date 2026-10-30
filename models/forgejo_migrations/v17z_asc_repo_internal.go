// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package forgejo_migrations

import (
	"code.forgejo.org/xorm/xorm"
)

func init() {
	registerMigration(&Migration{
		Description: "add repository.is_internal: a private repository open to every signed-in person of the instance",
		Upgrade:     addRepositoryIsInternal,
	})
}

func addRepositoryIsInternal(x *xorm.Engine) error {
	type Repository struct {
		ID         int64 `xorm:"pk autoincr"`
		IsInternal bool  `xorm:"NOT NULL DEFAULT false"`
	}
	_, err := x.SyncWithOptions(xorm.SyncOptions{IgnoreDropIndices: true}, new(Repository))
	return err
}
