// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package forgejo_migrations

import (
	"testing"

	migration_tests "forgejo.org/models/gitea_migrations/test"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_addProjectColumnHidden(t *testing.T) {
	type ProjectBoard struct {
		ID        int64 `xorm:"pk autoincr"`
		Title     string
		ProjectID int64
	}
	x, deferable := migration_tests.PrepareTestEnv(t, 0, new(ProjectBoard))
	defer deferable()
	if x == nil || t.Failed() {
		return
	}
	_, err := x.Exec("INSERT INTO `project_board` (`title`, `project_id`) VALUES ('Backlog', 1)")
	require.NoError(t, err)
	require.NoError(t, addProjectColumnHidden(x))
	require.NoError(t, addProjectColumnHidden(x))
	var row struct {
		Title  string
		Hidden bool
	}
	has, err := x.Table("project_board").Where("project_id = 1").Get(&row)
	require.NoError(t, err)
	assert.True(t, has)
	assert.Equal(t, "Backlog", row.Title)
	assert.False(t, row.Hidden, "an existing column stays shown")
}
