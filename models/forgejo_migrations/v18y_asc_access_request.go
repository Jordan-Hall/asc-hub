// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package forgejo_migrations

import (
	"code.forgejo.org/xorm/xorm"
)

func init() {
	registerMigration(&Migration{
		Description: "add asc_access_request: a request of a person for more access to a repository",
		Upgrade:     addAscAccessRequest,
	})
}

func addAscAccessRequest(x *xorm.Engine) error {
	type AscAccessRequest struct {
		ID          int64  `xorm:"pk autoincr"`
		RepoID      int64  `xorm:"UNIQUE(s) INDEX NOT NULL"`
		UserID      int64  `xorm:"UNIQUE(s) INDEX NOT NULL"`
		Mode        int    `xorm:"NOT NULL DEFAULT 2"`
		Message     string `xorm:"TEXT"`
		CreatedUnix int64  `xorm:"created"`
	}
	_, err := x.SyncWithOptions(xorm.SyncOptions{IgnoreDropIndices: true}, new(AscAccessRequest))
	return err
}
