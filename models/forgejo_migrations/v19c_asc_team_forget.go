// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package forgejo_migrations

import (
	"forgejo.org/modules/timeutil"

	"code.forgejo.org/xorm/xorm"
)

func init() {
	registerMigration(&Migration{
		Description: "add asc_team_forget: organization names whose teams asc must clear after a rename or delete",
		Upgrade:     addAscTeamForget,
	})
}

func addAscTeamForget(x *xorm.Engine) error {
	type AscTeamForget struct {
		ID          int64              `xorm:"pk autoincr"`
		OrgName     string             `xorm:"VARCHAR(255) UNIQUE NOT NULL"`
		CreatedUnix timeutil.TimeStamp `xorm:"created"`
	}
	_, err := x.SyncWithOptions(xorm.SyncOptions{IgnoreDropIndices: true}, new(AscTeamForget))
	return err
}
