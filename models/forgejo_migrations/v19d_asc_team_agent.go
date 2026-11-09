// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package forgejo_migrations

import (
	"forgejo.org/modules/timeutil"

	"code.forgejo.org/xorm/xorm"
)

func init() {
	registerMigration(&Migration{
		Description: "add asc_team_agent: agents of an organization that are members of its teams",
		Upgrade:     addAscTeamAgent,
	})
}

func addAscTeamAgent(x *xorm.Engine) error {
	type AscTeamAgent struct {
		ID          int64              `xorm:"pk autoincr"`
		OrgID       int64              `xorm:"INDEX NOT NULL"`
		TeamID      int64              `xorm:"UNIQUE(team_agent) NOT NULL"`
		Agent       string             `xorm:"VARCHAR(40) UNIQUE(team_agent) NOT NULL"`
		CreatedUnix timeutil.TimeStamp `xorm:"created"`
	}
	_, err := x.SyncWithOptions(xorm.SyncOptions{IgnoreDropIndices: true}, new(AscTeamAgent))
	return err
}
