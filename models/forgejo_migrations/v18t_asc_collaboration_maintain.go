// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package forgejo_migrations

import (
	"code.forgejo.org/xorm/xorm"
)

func init() {
	registerMigration(&Migration{
		Description: "add collaboration.asc_maintain: the Maintain role of a collaborator",
		Upgrade:     addAscCollaborationMaintain,
	})
}

type v18tCollaboration struct {
	ID          int64 `xorm:"pk autoincr"`
	RepoID      int64 `xorm:"UNIQUE(s) INDEX NOT NULL REFERENCES(repository, id)"`
	UserID      int64 `xorm:"UNIQUE(s) INDEX NOT NULL REFERENCES(user, id)"`
	Mode        int   `xorm:"DEFAULT 2 NOT NULL"`
	AscMaintain bool  `xorm:"NOT NULL DEFAULT false"`
	CreatedUnix int64 `xorm:"INDEX created"`
	UpdatedUnix int64 `xorm:"INDEX updated"`
}

func (v18tCollaboration) TableName() string { return "collaboration" }

func addAscCollaborationMaintain(x *xorm.Engine) error {
	return x.Sync(new(v18tCollaboration))
}
