// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package forgejo_migrations

import (
	"code.forgejo.org/xorm/xorm"
)

func init() {
	registerMigration(&Migration{
		Description: "add asc_repo_feature_off: the agent runs, dependencies and meaning search switches of a repository",
		Upgrade:     addAscRepoFeatureOff,
	})
}

type v18rAscRepoFeatureOff struct {
	ID      int64  `xorm:"pk autoincr"`
	RepoID  int64  `xorm:"UNIQUE(repo_feature) NOT NULL"`
	Feature string `xorm:"UNIQUE(repo_feature) VARCHAR(32) NOT NULL"`
}

func (v18rAscRepoFeatureOff) TableName() string { return "asc_repo_feature_off" }

func addAscRepoFeatureOff(x *xorm.Engine) error {
	return x.Sync(new(v18rAscRepoFeatureOff))
}
