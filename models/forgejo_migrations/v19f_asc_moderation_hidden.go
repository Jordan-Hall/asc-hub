// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package forgejo_migrations

import (
	"forgejo.org/modules/timeutil"

	"code.forgejo.org/xorm/xorm"
)

func init() {
	registerMigration(&Migration{
		Description: "add asc_moderation_hidden: reported content an administrator hid, and what hiding changed",
		Upgrade:     addAscModerationHidden,
	})
}

type v19fHidden struct {
	ID          int64              `xorm:"pk autoincr"`
	ContentType int                `xorm:"UNIQUE(s) NOT NULL"`
	ContentID   int64              `xorm:"UNIQUE(s) NOT NULL"`
	Saved       string             `xorm:"LONGTEXT"`
	ModeratorID int64              `xorm:"NOT NULL"`
	CreatedUnix timeutil.TimeStamp `xorm:"created NOT NULL"`
}

func (v19fHidden) TableName() string { return "asc_moderation_hidden" }

func addAscModerationHidden(x *xorm.Engine) error {
	return x.Sync(new(v19fHidden))
}
