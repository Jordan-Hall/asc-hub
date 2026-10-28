// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package forgejo_migrations

import (
	"code.forgejo.org/xorm/xorm"
)

func init() {
	registerMigration(&Migration{
		Description: "add asc_trending_repo: the weekly score of public repositories, kept by a cron task",
		Upgrade:     addAscTrendingRepo,
	})
}

func addAscTrendingRepo(x *xorm.Engine) error {
	type AscTrendingRepo struct {
		RepoID       int64 `xorm:"pk"`
		Score        int64 `xorm:"INDEX NOT NULL DEFAULT 0"`
		Stars        int64 `xorm:"NOT NULL DEFAULT 0"`
		Forks        int64 `xorm:"NOT NULL DEFAULT 0"`
		Activity     int64 `xorm:"NOT NULL DEFAULT 0"`
		ComputedUnix int64 `xorm:"NOT NULL DEFAULT 0"`
	}
	_, err := x.SyncWithOptions(xorm.SyncOptions{IgnoreDropIndices: true}, new(AscTrendingRepo))
	return err
}
