// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package forgejo_migrations

import (
	"testing"

	migration_tests "forgejo.org/models/gitea_migrations/test"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_addAscDeviceCode(t *testing.T) {
	type User struct {
		ID int64 `xorm:"pk autoincr"`
	}
	x, deferable := migration_tests.PrepareTestEnv(t, 0, new(User))
	defer deferable()
	if x == nil || t.Failed() {
		return
	}

	// The migration can run twice: a repeated run changes nothing.
	require.NoError(t, addAscDeviceCode(x))
	require.NoError(t, addAscDeviceCode(x))

	_, err := x.Exec("INSERT INTO `asc_device_code` (`device_code_hash`, `user_code`, `expires_unix`) VALUES ('h1', 'BCDFGHJK', 100)")
	require.NoError(t, err)
	// both the device code hash and the user code are unique
	_, err = x.Exec("INSERT INTO `asc_device_code` (`device_code_hash`, `user_code`, `expires_unix`) VALUES ('h1', 'LMNPQRST', 100)")
	assert.Error(t, err)
	_, err = x.Exec("INSERT INTO `asc_device_code` (`device_code_hash`, `user_code`, `expires_unix`) VALUES ('h2', 'BCDFGHJK', 100)")
	assert.Error(t, err)

	// a new code is pending and unpolled
	var status, user, polled int64
	has, err := x.SQL("SELECT `status`, `user_id`, `last_poll_unix` FROM `asc_device_code` WHERE `user_code` = 'BCDFGHJK'").Get(&status, &user, &polled)
	require.NoError(t, err)
	assert.True(t, has)
	assert.Zero(t, status+user+polled)
}
