// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package forgejo_migrations

import (
	"code.forgejo.org/xorm/xorm"
)

func init() {
	registerMigration(&Migration{
		Description: "add pull_request_asc_dirs: the directories a pull request changes, for Knows best on a profile",
		Upgrade:     addPullRequestAscDirs,
	})
}

func addPullRequestAscDirs(x *xorm.Engine) error {
	type PullRequestAscDirs struct {
		ID     int64          `xorm:"pk autoincr"`
		PullID int64          `xorm:"UNIQUE NOT NULL"`
		Head   string         `xorm:"VARCHAR(64) NOT NULL"`
		Dirs   map[string]int `xorm:"JSON TEXT"`
	}
	_, err := x.SyncWithOptions(xorm.SyncOptions{IgnoreDropIndices: true}, new(PullRequestAscDirs))
	return err
}
