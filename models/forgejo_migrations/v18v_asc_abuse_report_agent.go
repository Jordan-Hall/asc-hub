// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package forgejo_migrations

import (
	"code.forgejo.org/xorm/xorm"
)

func init() {
	registerMigration(&Migration{
		Description: "add reporter_agent and reporter_agent_token to abuse_report: reports an agent sends with its token",
		Upgrade:     addAscAbuseReportAgent,
	})
}

type v18vAbuseReport struct {
	ReporterAgent      string `xorm:"VARCHAR(64) NOT NULL DEFAULT ''"`
	ReporterAgentToken string `xorm:"VARCHAR(64) INDEX NOT NULL DEFAULT ''"`
}

func (v18vAbuseReport) TableName() string { return "abuse_report" }

func addAscAbuseReportAgent(x *xorm.Engine) error {
	return x.Sync(new(v18vAbuseReport))
}
