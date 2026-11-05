// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package forgejo_migrations

import (
	"forgejo.org/modules/timeutil"

	"code.forgejo.org/xorm/xorm"
)

func init() {
	registerMigration(&Migration{
		Description: "add asc_docs_feedback: the answers to Was this page helpful? on the documentation",
		Upgrade:     addAscDocsFeedback,
	})
}

func addAscDocsFeedback(x *xorm.Engine) error {
	type AscDocsFeedback struct {
		ID          int64              `xorm:"pk autoincr"`
		Slug        string             `xorm:"VARCHAR(100) INDEX NOT NULL"`
		Helpful     bool               `xorm:"NOT NULL"`
		CreatedUnix timeutil.TimeStamp `xorm:"created INDEX"`
	}
	return x.Sync(new(AscDocsFeedback))
}
