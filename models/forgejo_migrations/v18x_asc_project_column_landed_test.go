// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package forgejo_migrations

import (
	"testing"

	migration_tests "forgejo.org/models/gitea_migrations/test"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_addProjectColumnLanded(t *testing.T) {
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
	_, err := x.Exec("INSERT INTO `project_board` (`title`, `project_id`) VALUES ('Done', 1)")
	require.NoError(t, err)
	require.NoError(t, addProjectColumnLanded(x))
	require.NoError(t, addProjectColumnLanded(x), "running twice changes nothing")
	var row struct {
		Title     string
		AscLanded bool
	}
	has, err := x.Table("project_board").Where("project_id = 1").Get(&row)
	require.NoError(t, err)
	assert.True(t, has)
	assert.Equal(t, "Done", row.Title)
	assert.False(t, row.AscLanded, "an existing column is not the Landed column")
}
