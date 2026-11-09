// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package forgejo_migrations

import (
	"forgejo.org/modules/timeutil"

	"code.forgejo.org/xorm/xorm"
)

func init() {
	registerMigration(&Migration{
		Description: "add asc_moderation_decision: each moderation action, its reason and the owner's appeal",
		Upgrade:     addAscModerationDecision,
	})
}

type v18uDecision struct {
	ID          int64              `xorm:"pk autoincr"`
	ContentType int                `xorm:"NOT NULL"`
	ContentID   int64              `xorm:"NOT NULL"`
	ContentRef  string             `xorm:"VARCHAR(255)"`
	OwnerID     int64              `xorm:"INDEX NOT NULL"`
	Action      int                `xorm:"NOT NULL"`
	Reason      string             `xorm:"VARCHAR(500)"`
	ModeratorID int64              `xorm:"NOT NULL"`
	CreatedUnix timeutil.TimeStamp `xorm:"created INDEX NOT NULL"`

	AppealStatus      int                `xorm:"INDEX NOT NULL DEFAULT 0"`
	AppealText        string             `xorm:"VARCHAR(1000)"`
	AppealUnix        timeutil.TimeStamp `xorm:"NOT NULL DEFAULT 0"`
	AppealDeciderID   int64              `xorm:"NOT NULL DEFAULT 0"`
	AppealReason      string             `xorm:"VARCHAR(500)"`
	AppealDecidedUnix timeutil.TimeStamp `xorm:"NOT NULL DEFAULT 0"`
}

func (v18uDecision) TableName() string { return "asc_moderation_decision" }

func addAscModerationDecision(x *xorm.Engine) error {
	return x.Sync(new(v18uDecision))
}
