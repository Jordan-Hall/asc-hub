// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package forgejo_migrations

import "code.forgejo.org/xorm/xorm"

func init() {
	registerMigration(&Migration{
		Description: "add deploy_key.added_by_id: who added a deploy key, shown on the access page",
		Upgrade:     addDeployKeyAddedBy,
	})
}

func addDeployKeyAddedBy(x *xorm.Engine) error {
	type DeployKey struct {
		AddedByID int64 `xorm:"NOT NULL DEFAULT 0"`
	}
	_, err := x.SyncWithOptions(xorm.SyncOptions{IgnoreDropIndices: true}, new(DeployKey))
	return err
}
