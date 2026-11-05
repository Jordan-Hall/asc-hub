// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package forgejo_migrations

import (
	"testing"

	migration_tests "forgejo.org/models/gitea_migrations/test"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_addAscNotifyLogRead(t *testing.T) {
	type User struct {
		ID int64 `xorm:"pk autoincr"`
	}
	x, deferable := migration_tests.PrepareTestEnv(t, 0, new(User))
	defer deferable()
	if x == nil || t.Failed() {
		return
	}
	require.NoError(t, addAscNotifyLog(x))
	_, err := x.Exec("INSERT INTO `asc_notify_log` (`user_id`, `repo_id`, `event`, `sent_unix`) VALUES (1, 2, 'checks_failed', 10)")
	require.NoError(t, err)
	require.NoError(t, addAscNotifyLogRead(x))
	require.NoError(t, addAscNotifyLogRead(x))
	var detail string
	var read int64
	has, err := x.SQL("SELECT `detail`, `read_unix` FROM `asc_notify_log` WHERE user_id = 1").Get(&detail, &read)
	require.NoError(t, err)
	require.True(t, has)
	assert.Empty(t, detail)
	assert.EqualValues(t, 10, read, "an event told before the popover existed counts as read")
}
