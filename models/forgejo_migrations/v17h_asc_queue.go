// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package forgejo_migrations

import (
	"code.forgejo.org/xorm/xorm"
)

func init() {
	registerMigration(&Migration{
		Description: "add asc_stack_queue and asc_stack_queue_branch: the merge-when-ready queue of stacked pull requests",
		Upgrade:     addAscStackQueue,
	})
}

func addAscStackQueue(x *xorm.Engine) error {
	type AscStackQueue struct {
		ID          int64  `xorm:"pk autoincr"`
		RepoID      int64  `xorm:"INDEX(repo_branch) NOT NULL"`
		Branch      string `xorm:"VARCHAR(255) INDEX(repo_branch) NOT NULL"`
		PullID      int64  `xorm:"UNIQUE NOT NULL"`
		AscChange   string `xorm:"VARCHAR(32) NOT NULL"`
		DoerID      int64  `xorm:"NOT NULL"`
		State       string `xorm:"VARCHAR(16) INDEX NOT NULL"`
		Reason      string `xorm:"VARCHAR(255) NOT NULL DEFAULT ''"`
		BlockedHead string `xorm:"VARCHAR(64) NOT NULL DEFAULT ''"`
		CreatedUnix int64  `xorm:"created NOT NULL"`
		UpdatedUnix int64  `xorm:"updated NOT NULL"`
	}
	type AscStackQueueBranch struct {
		ID         int64  `xorm:"pk autoincr"`
		RepoID     int64  `xorm:"UNIQUE(repo_branch) NOT NULL"`
		Branch     string `xorm:"VARCHAR(255) UNIQUE(repo_branch) NOT NULL"`
		Paused     bool   `xorm:"NOT NULL DEFAULT false"`
		PausedByID int64  `xorm:"NOT NULL DEFAULT 0"`
		LockUntil  int64  `xorm:"NOT NULL DEFAULT 0"`
	}
	if _, err := x.SyncWithOptions(xorm.SyncOptions{IgnoreDropIndices: true}, new(AscStackQueue)); err != nil {
		return err
	}
	_, err := x.SyncWithOptions(xorm.SyncOptions{IgnoreDropIndices: true}, new(AscStackQueueBranch))
	return err
}
