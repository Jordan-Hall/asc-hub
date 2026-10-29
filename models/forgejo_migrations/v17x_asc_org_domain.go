// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package forgejo_migrations

import (
	"code.forgejo.org/xorm/xorm"
)

func init() {
	registerMigration(&Migration{
		Description: "add asc_org_domain: the domain an organization proved with a DNS record",
		Upgrade:     addAscOrgDomain,
	})
}

func addAscOrgDomain(x *xorm.Engine) error {
	type AscOrgDomain struct {
		ID           int64  `xorm:"pk autoincr"`
		OrgID        int64  `xorm:"UNIQUE NOT NULL"`
		Domain       string `xorm:"VARCHAR(253) INDEX NOT NULL"`
		Token        string `xorm:"VARCHAR(64) NOT NULL"`
		VerifiedUnix int64  `xorm:"NOT NULL DEFAULT 0"`
		CheckedUnix  int64  `xorm:"NOT NULL DEFAULT 0"`
	}
	_, err := x.SyncWithOptions(xorm.SyncOptions{IgnoreDropIndices: true}, new(AscOrgDomain))
	return err
}
