// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package forgejo_migrations

import (
	"testing"

	migration_tests "forgejo.org/models/gitea_migrations/test"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_addActionTaskPermissions(t *testing.T) {
	type ActionTask struct {
		ID     int64 `xorm:"pk autoincr"`
		RepoID int64
	}
	x, deferable := migration_tests.PrepareTestEnv(t, 0, new(ActionTask))
	defer deferable()
	if x == nil || t.Failed() {
		return
	}

	_, err := x.Insert(&ActionTask{RepoID: 1})
	require.NoError(t, err)

	require.NoError(t, addActionTaskPermissions(x))
	// A second run is harmless.
	require.NoError(t, addActionTaskPermissions(x))

	// A task that started before has no permissions and keeps the default capability.
	var rows []map[string]string
	require.NoError(t, x.Table("action_task").Select("`repo_id`, `permissions`").Find(&rows))
	assert.Equal(t, []map[string]string{{"repo_id": "1", "permissions": ""}}, rows)

	_, err = x.Exec("UPDATE `action_task` SET `permissions` = ?", `{"contents":"read"}`)
	require.NoError(t, err)
	var permissions string
	has, err := x.Table("action_task").Select("`permissions`").Get(&permissions)
	require.NoError(t, err)
	require.True(t, has)
	assert.JSONEq(t, `{"contents":"read"}`, permissions)
}
