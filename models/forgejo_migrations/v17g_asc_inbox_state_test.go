// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package forgejo_migrations

import (
	"testing"

	migration_tests "forgejo.org/models/gitea_migrations/test"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_addAscInboxState(t *testing.T) {
	type PullRequest struct {
		ID int64 `xorm:"pk autoincr"`
	}
	x, deferable := migration_tests.PrepareTestEnv(t, 0, new(PullRequest))
	defer deferable()
	if x == nil || t.Failed() {
		return
	}

	// The migration can run twice: a repeated run changes nothing.
	require.NoError(t, addAscInboxState(x))
	require.NoError(t, addAscInboxState(x))

	_, err := x.Exec("INSERT INTO `asc_inbox_state` (`user_id`, `pull_id`, `pinned`, `snoozed_until`, `updated_unix`) VALUES (1, 10, ?, 0, 0)", true)
	require.NoError(t, err)
	// A user has one row per pull request, and another user's row does not collide.
	_, err = x.Exec("INSERT INTO `asc_inbox_state` (`user_id`, `pull_id`, `pinned`, `snoozed_until`, `updated_unix`) VALUES (1, 10, ?, 0, 0)", false)
	assert.Error(t, err)
	_, err = x.Exec("INSERT INTO `asc_inbox_state` (`user_id`, `pull_id`, `pinned`, `snoozed_until`, `updated_unix`) VALUES (2, 10, ?, 0, 0)", false)
	require.NoError(t, err)

	// The defaults mean "not pinned, not snoozed".
	_, err = x.Exec("INSERT INTO `asc_inbox_state` (`user_id`, `pull_id`) VALUES (3, 11)")
	require.NoError(t, err)
	var pinned bool
	var snoozed int64
	ok, err := x.SQL("SELECT `pinned`, `snoozed_until` FROM `asc_inbox_state` WHERE `user_id` = 3 AND `pull_id` = 11").Get(&pinned, &snoozed)
	require.NoError(t, err)
	require.True(t, ok)
	assert.False(t, pinned)
	assert.Zero(t, snoozed)
}
