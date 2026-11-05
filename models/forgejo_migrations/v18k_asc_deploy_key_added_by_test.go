// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package forgejo_migrations

import (
	"testing"

	migration_tests "forgejo.org/models/gitea_migrations/test"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_addDeployKeyAddedBy(t *testing.T) {
	type DeployKey struct {
		ID     int64 `xorm:"pk autoincr"`
		KeyID  int64 `xorm:"UNIQUE(s) INDEX"`
		RepoID int64 `xorm:"UNIQUE(s) INDEX"`
		Name   string
	}
	x, deferable := migration_tests.PrepareTestEnv(t, 0, new(DeployKey))
	defer deferable()
	if x == nil || t.Failed() {
		return
	}
	_, err := x.Exec("INSERT INTO `deploy_key` (`key_id`, `repo_id`, `name`) VALUES (1, 1, 'ci')")
	require.NoError(t, err)
	require.NoError(t, addDeployKeyAddedBy(x))
	require.NoError(t, addDeployKeyAddedBy(x))
	var ids []int64
	require.NoError(t, x.Table("deploy_key").Cols("added_by_id").Find(&ids))
	assert.Equal(t, []int64{0}, ids, "a key added before the column has no recorded person")
}
