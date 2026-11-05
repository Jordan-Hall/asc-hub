// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package forgejo_migrations

import (
	"testing"

	migration_tests "forgejo.org/models/gitea_migrations/test"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_addAscInboxDone(t *testing.T) {
	type AscInboxState struct {
		ID           int64 `xorm:"pk autoincr"`
		UserID       int64 `xorm:"UNIQUE(user_pull) NOT NULL"`
		PullID       int64 `xorm:"UNIQUE(user_pull) INDEX NOT NULL"`
		Pinned       bool  `xorm:"NOT NULL DEFAULT false"`
		SnoozedUntil int64 `xorm:"NOT NULL DEFAULT 0"`
		UpdatedUnix  int64 `xorm:"updated"`
	}
	x, deferable := migration_tests.PrepareTestEnv(t, 0, new(AscInboxState))
	defer deferable()
	if x == nil || t.Failed() {
		return
	}
	_, err := x.Exec("INSERT INTO `asc_inbox_state` (`user_id`, `pull_id`, `pinned`, `snoozed_until`) VALUES (1, 2, true, 0)")
	require.NoError(t, err)
	require.NoError(t, addAscInboxDone(x))
	require.NoError(t, addAscInboxDone(x))
	var done []int64
	require.NoError(t, x.Table("asc_inbox_state").Cols("done_unix").Find(&done))
	assert.Equal(t, []int64{0}, done, "a mark set before the column is not done")
}
