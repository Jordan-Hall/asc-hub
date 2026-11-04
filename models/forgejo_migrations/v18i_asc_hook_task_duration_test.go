// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package forgejo_migrations

import (
	"testing"

	migration_tests "forgejo.org/models/gitea_migrations/test"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_addHookTaskDuration(t *testing.T) {
	type HookTask struct {
		ID     int64 `xorm:"pk autoincr"`
		HookID int64 `xorm:"index"`
		UUID   string
	}
	x, deferable := migration_tests.PrepareTestEnv(t, 0, new(HookTask))
	defer deferable()
	if x == nil || t.Failed() {
		return
	}
	_, err := x.Exec("INSERT INTO `hook_task` (`hook_id`, `uuid`) VALUES (1, 'a')")
	require.NoError(t, err)
	require.NoError(t, addHookTaskDuration(x))
	require.NoError(t, addHookTaskDuration(x))
	var ms []int64
	require.NoError(t, x.Table("hook_task").Cols("duration_ms").Find(&ms))
	assert.Equal(t, []int64{0}, ms, "a delivery made before the column has no duration")
}
