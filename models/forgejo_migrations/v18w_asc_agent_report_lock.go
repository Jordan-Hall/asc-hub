// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package forgejo_migrations

import (
	"code.forgejo.org/xorm/xorm"
)

func init() {
	registerMigration(&Migration{
		Description: "add asc_agent_report_lock: one row per agent token, locked while a report of the token is stored",
		Upgrade:     addAscAgentReportLock,
	})
}

type v18wAgentReportLock struct {
	Token string `xorm:"VARCHAR(64) PK"`
	Seq   int64  `xorm:"NOT NULL DEFAULT 0"`
}

func (v18wAgentReportLock) TableName() string { return "asc_agent_report_lock" }

func addAscAgentReportLock(x *xorm.Engine) error {
	return x.Sync(new(v18wAgentReportLock))
}
