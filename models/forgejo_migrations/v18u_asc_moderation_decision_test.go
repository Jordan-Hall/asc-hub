// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package forgejo_migrations

import (
	"testing"

	migration_tests "forgejo.org/models/gitea_migrations/test"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_addAscModerationDecision(t *testing.T) {
	x, deferable := migration_tests.PrepareTestEnv(t, 0)
	defer deferable()
	if x == nil || t.Failed() {
		return
	}
	require.NoError(t, addAscModerationDecision(x))
	require.NoError(t, addAscModerationDecision(x), "running twice changes nothing")
	_, err := x.Exec("INSERT INTO `asc_moderation_decision` (`content_type`, `content_id`, `owner_id`, `action`, `moderator_id`, `created_unix`) VALUES (1, 5, 5, 1, 1, 10)")
	require.NoError(t, err)
	var status []int
	require.NoError(t, x.Table("asc_moderation_decision").Cols("appeal_status").Find(&status))
	assert.Equal(t, []int{0}, status, "a new decision has no appeal")
}
