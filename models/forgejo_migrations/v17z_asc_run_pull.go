// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package forgejo_migrations

import (
	"code.forgejo.org/xorm/xorm"
)

func init() {
	registerMigration(&Migration{
		Description: "add pull_request_asc_run: the agent run a pull request was opened for",
		Upgrade:     addPullRequestAscRun,
	})
}

func addPullRequestAscRun(x *xorm.Engine) error {
	type PullRequestAscRun struct {
		ID        int64  `xorm:"pk autoincr"`
		PullID    int64  `xorm:"UNIQUE NOT NULL"`
		RepoID    int64  `xorm:"INDEX(repo_run) NOT NULL"`
		RunID     uint64 `xorm:"INDEX(repo_run) NOT NULL"`
		Agent     string `xorm:"VARCHAR(40) NOT NULL"`
		StartedBy string `xorm:"VARCHAR(255) NOT NULL DEFAULT ''"`
	}
	_, err := x.SyncWithOptions(xorm.SyncOptions{IgnoreDropIndices: true}, new(PullRequestAscRun))
	return err
}
