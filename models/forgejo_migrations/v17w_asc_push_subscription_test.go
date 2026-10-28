// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package forgejo_migrations

import (
	"testing"

	migration_tests "forgejo.org/models/gitea_migrations/test"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_addAscPushSubscription(t *testing.T) {
	type User struct {
		ID int64 `xorm:"pk autoincr"`
	}
	x, deferable := migration_tests.PrepareTestEnv(t, 0, new(User))
	defer deferable()
	if x == nil || t.Failed() {
		return
	}
	require.NoError(t, addAscPushSubscription(x))
	require.NoError(t, addAscPushSubscription(x))
	_, err := x.Exec("INSERT INTO `asc_push_subscription` (`user_id`, `endpoint_hash`, `endpoint`, `p256dh`, `auth`) VALUES (1, 'h', 'https://e', 'k', 'a')")
	require.NoError(t, err)
	_, err = x.Exec("INSERT INTO `asc_push_subscription` (`user_id`, `endpoint_hash`, `endpoint`, `p256dh`, `auth`) VALUES (2, 'h', 'https://e', 'k', 'a')")
	assert.Error(t, err, "one row for each endpoint")
}
