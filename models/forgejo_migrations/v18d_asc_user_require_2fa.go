// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package forgejo_migrations

import (
	"code.forgejo.org/xorm/xorm"
)

func init() {
	registerMigration(&Migration{
		Description: "add require_two_factor to user: an administrator can require a second factor of one person",
		Upgrade:     addUserRequireTwoFactor,
	})
}

func addUserRequireTwoFactor(x *xorm.Engine) error {
	type User struct {
		ID               int64
		RequireTwoFactor bool `xorm:"NOT NULL DEFAULT false"`
	}
	_, err := x.SyncWithOptions(xorm.SyncOptions{IgnoreDropIndices: true}, new(User))
	return err
}
