// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package forgejo_migrations

import (
	"code.forgejo.org/xorm/xorm"
)

func init() {
	registerMigration(&Migration{
		Description: "add asc_removed_rule: removed branch rules kept for 30 minutes so they can be put back",
		Upgrade:     addAscRemovedRule,
	})
}

type v18sAscRemovedRule struct {
	ID          int64  `xorm:"pk autoincr"`
	RepoID      int64  `xorm:"INDEX NOT NULL"`
	RuleName    string `xorm:"NOT NULL"`
	Rule        string `xorm:"LONGTEXT NOT NULL"`
	RemovedBy   int64  `xorm:"NOT NULL DEFAULT 0"`
	RemovedUnix int64  `xorm:"INDEX NOT NULL"`
}

func (v18sAscRemovedRule) TableName() string { return "asc_removed_rule" }

func addAscRemovedRule(x *xorm.Engine) error {
	return x.Sync(new(v18sAscRemovedRule))
}
