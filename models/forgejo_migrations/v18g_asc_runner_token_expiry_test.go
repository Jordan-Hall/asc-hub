// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package forgejo_migrations

import (
	"testing"

	migration_tests "forgejo.org/models/gitea_migrations/test"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_addRunnerTokenExpiry(t *testing.T) {
	type ActionRunnerToken struct {
		ID       int64  `xorm:"pk autoincr"`
		Token    string `xorm:"UNIQUE"`
		IsActive bool
	}
	x, deferable := migration_tests.PrepareTestEnv(t, 0, new(ActionRunnerToken))
	defer deferable()
	if x == nil || t.Failed() {
		return
	}
	_, err := x.Exec("INSERT INTO `action_runner_token` (`token`, `is_active`) VALUES ('nova-token', true)")
	require.NoError(t, err)
	require.NoError(t, addRunnerTokenExpiry(x))
	require.NoError(t, addRunnerTokenExpiry(x))
	var row struct {
		Token       string
		ExpiresUnix int64
	}
	has, err := x.Table("action_runner_token").Where("token = 'nova-token'").Get(&row)
	require.NoError(t, err)
	assert.True(t, has)
	assert.Zero(t, row.ExpiresUnix, "a token made before keeps no expiry")
}
