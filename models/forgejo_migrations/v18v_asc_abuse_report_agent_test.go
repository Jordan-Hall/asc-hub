// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package forgejo_migrations

import (
	"testing"

	migration_tests "forgejo.org/models/gitea_migrations/test"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type v18vOldAbuseReport struct {
	ID          int64 `xorm:"pk autoincr"`
	Status      int   `xorm:"INDEX NOT NULL DEFAULT 1"`
	ReporterID  int64 `xorm:"NOT NULL"`
	ContentType int   `xorm:"INDEX NOT NULL"`
	ContentID   int64 `xorm:"NOT NULL"`
	Category    int   `xorm:"INDEX NOT NULL"`
}

func (v18vOldAbuseReport) TableName() string { return "abuse_report" }

func Test_addAscAbuseReportAgent(t *testing.T) {
	x, deferable := migration_tests.PrepareTestEnv(t, 0, new(v18vOldAbuseReport))
	defer deferable()
	if x == nil || t.Failed() {
		return
	}
	_, err := x.Insert(&v18vOldAbuseReport{Status: 1, ReporterID: 2, ContentType: 2, ContentID: 1, Category: 2})
	require.NoError(t, err)
	require.NoError(t, addAscAbuseReportAgent(x))
	require.NoError(t, addAscAbuseReportAgent(x), "running twice changes nothing")
	var tokens []string
	require.NoError(t, x.Table("abuse_report").Cols("reporter_agent_token").Find(&tokens))
	assert.Equal(t, []string{""}, tokens, "a report of a person has no agent token")
}
