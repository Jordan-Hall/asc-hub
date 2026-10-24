// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package forgejo_migrations

import (
	"code.forgejo.org/xorm/xorm"
)

func init() {
	registerMigration(&Migration{
		Description: "add pull_request_asc_stack: the asc change and the stack parent of a pull request",
		Upgrade:     addPullRequestAscStack,
	})
}

func addPullRequestAscStack(x *xorm.Engine) error {
	type PullRequestAscStack struct {
		ID            int64  `xorm:"pk autoincr"`
		PullID        int64  `xorm:"UNIQUE NOT NULL"`
		RepoID        int64  `xorm:"INDEX(repo_change) NOT NULL"`
		AscChange     string `xorm:"VARCHAR(32) INDEX(repo_change) NOT NULL"`
		StackParentID int64  `xorm:"INDEX NOT NULL DEFAULT 0"`
	}
	// Existing pull requests have no row: they are not stacked.
	_, err := x.SyncWithOptions(xorm.SyncOptions{IgnoreDropIndices: true}, new(PullRequestAscStack))
	return err
}
