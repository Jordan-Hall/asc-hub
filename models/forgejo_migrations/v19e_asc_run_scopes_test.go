// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package forgejo_migrations

import (
	"testing"

	migration_tests "forgejo.org/models/gitea_migrations/test"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_addAscRunScopes(t *testing.T) {
	type Secret struct {
		ID   int64  `xorm:"pk autoincr"`
		Name string `xorm:"NOT NULL"`
	}
	type ActionVariable struct {
		ID   int64  `xorm:"pk autoincr"`
		Name string `xorm:"NOT NULL"`
	}
	x, deferable := migration_tests.PrepareTestEnv(t, 0, new(Secret), new(ActionVariable))
	defer deferable()
	if x == nil || t.Failed() {
		return
	}
	_, err := x.Exec("INSERT INTO `secret` (`name`) VALUES ('OLD')")
	require.NoError(t, err)
	_, err = x.Exec("INSERT INTO `action_variable` (`name`) VALUES ('OLD')")
	require.NoError(t, err)

	require.NoError(t, addAscRunScopes(x))
	require.NoError(t, addAscRunScopes(x), "running it again changes nothing")

	var scopes int
	_, err = x.SQL("SELECT `scopes` FROM `secret` WHERE `name` = 'OLD'").Get(&scopes)
	require.NoError(t, err)
	assert.Equal(t, 3, scopes, "an old secret reaches checks and releases, not agents")
	var v struct {
		Scopes    int
		UpdaterID int64
	}
	_, err = x.SQL("SELECT `scopes`, `updater_id` FROM `action_variable` WHERE `name` = 'OLD'").Get(&v.Scopes, &v.UpdaterID)
	require.NoError(t, err)
	assert.Equal(t, 3, v.Scopes)
	assert.Zero(t, v.UpdaterID)
}
