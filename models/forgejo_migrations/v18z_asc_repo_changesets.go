// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package forgejo_migrations

import (
	"code.forgejo.org/xorm/xorm"
)

func init() {
	registerMigration(&Migration{
		Description: "add asc_repo_changesets: the changeset count of the default branch of a repository",
		Upgrade:     addAscRepoChangesets,
	})
}

func addAscRepoChangesets(x *xorm.Engine) error {
	type AscRepoChangesets struct {
		RepoID      int64  `xorm:"pk"`
		CommitSha   string `xorm:"VARCHAR(64) NOT NULL DEFAULT ''"`
		Changesets  int64  `xorm:"NOT NULL DEFAULT 0"`
		UpdatedUnix int64  `xorm:"updated"`
	}
	_, err := x.SyncWithOptions(xorm.SyncOptions{IgnoreDropIndices: true}, new(AscRepoChangesets))
	return err
}
