// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package forgejo_migrations

import (
	"forgejo.org/modules/timeutil"

	"code.forgejo.org/xorm/xorm"
)

func init() {
	registerMigration(&Migration{
		Description: "add asc_babysit: the changes whose failing checks an agent is asked to fix",
		Upgrade:     addAscBabysit,
	})
}

func addAscBabysit(x *xorm.Engine) error {
	type AscBabysit struct {
		ID          int64              `xorm:"pk autoincr"`
		PullID      int64              `xorm:"UNIQUE NOT NULL"`
		RepoID      int64              `xorm:"INDEX NOT NULL"`
		Agent       string             `xorm:"VARCHAR(40) NOT NULL"`
		DoerID      int64              `xorm:"NOT NULL"`
		Runs        int                `xorm:"NOT NULL DEFAULT 0"`
		LastSHA     string             `xorm:"VARCHAR(64) NOT NULL DEFAULT ''"`
		CreatedUnix timeutil.TimeStamp `xorm:"created NOT NULL"`
		UpdatedUnix timeutil.TimeStamp `xorm:"updated NOT NULL"`
	}
	_, err := x.SyncWithOptions(xorm.SyncOptions{IgnoreDropIndices: true}, new(AscBabysit))
	return err
}
