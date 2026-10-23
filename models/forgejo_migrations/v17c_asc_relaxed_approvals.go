// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package forgejo_migrations

import (
	"code.forgejo.org/xorm/xorm"
)

func init() {
	registerMigration(&Migration{
		Description: "add AscRelaxedClasses and AscRelaxedApprovals to ProtectedBranch",
		Upgrade:     addProtectedBranchAscRelaxedApprovals,
	})
}

func addProtectedBranchAscRelaxedApprovals(x *xorm.Engine) error {
	type ProtectedBranch struct {
		AscRelaxedClasses   []string `xorm:"JSON TEXT"`
		AscRelaxedApprovals int64    `xorm:"NOT NULL DEFAULT 0"`
	}
	// Existing rules get no relaxation: an empty class list turns it off.
	_, err := x.SyncWithOptions(xorm.SyncOptions{IgnoreDropIndices: true}, new(ProtectedBranch))
	return err
}
