// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package forgejo_migrations

import (
	"code.forgejo.org/xorm/xorm"
)

func init() {
	registerMigration(&Migration{
		Description: "add asc_repo_defaults: who may create repositories in an organization, the base permission of its members and the default visibility of a new repository",
		Upgrade:     addAscRepoDefaults,
	})
}

func addAscRepoDefaults(x *xorm.Engine) error {
	type AscRepoDefaults struct {
		OrgID          int64  `xorm:"pk"`
		RepoCreators   string `xorm:"VARCHAR(16) NOT NULL DEFAULT 'teams'"`
		BasePermission int    `xorm:"NOT NULL DEFAULT 0"`
		Visibility     string `xorm:"VARCHAR(16) NOT NULL DEFAULT ''"`
		UpdatedUnix    int64  `xorm:"updated"`
	}
	_, err := x.SyncWithOptions(xorm.SyncOptions{IgnoreDropIndices: true}, new(AscRepoDefaults))
	return err
}
