// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package forgejo_migrations

import (
	"testing"

	migration_tests "forgejo.org/models/gitea_migrations/test"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_addSecretUpdatedBy(t *testing.T) {
	type Secret struct {
		ID          int64  `xorm:"pk autoincr"`
		OwnerID     int64  `xorm:"INDEX UNIQUE(owner_repo_name) NOT NULL"`
		RepoID      int64  `xorm:"INDEX UNIQUE(owner_repo_name) NOT NULL DEFAULT 0"`
		Name        string `xorm:"UNIQUE(owner_repo_name) NOT NULL"`
		Data        []byte `xorm:"BLOB"`
		CreatedUnix int64  `xorm:"NOT NULL"`
	}
	x, deferable := migration_tests.PrepareTestEnv(t, 0, new(Secret))
	defer deferable()
	if x == nil || t.Failed() {
		return
	}
	_, err := x.Exec("INSERT INTO `secret` (`owner_id`, `repo_id`, `name`, `created_unix`) VALUES (0, 1, 'TOKEN', 1700000000)")
	require.NoError(t, err)

	require.NoError(t, addSecretUpdatedBy(x))
	require.NoError(t, addSecretUpdatedBy(x))

	var row struct {
		UpdatedUnix int64
		UpdaterID   int64
	}
	has, err := x.SQL("SELECT `updated_unix`, `updater_id` FROM `secret` WHERE `name` = 'TOKEN'").Get(&row)
	require.NoError(t, err)
	require.True(t, has)
	assert.EqualValues(t, 1700000000, row.UpdatedUnix)
	assert.EqualValues(t, 0, row.UpdaterID)
}
