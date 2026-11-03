// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package forgejo_migrations

import (
	"testing"

	migration_tests "forgejo.org/models/gitea_migrations/test"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_addAscRepoDefaults(t *testing.T) {
	type User struct {
		ID int64 `xorm:"pk autoincr"`
	}
	x, deferable := migration_tests.PrepareTestEnv(t, 0, new(User))
	defer deferable()
	if x == nil || t.Failed() {
		return
	}
	require.NoError(t, addAscRepoDefaults(x))
	require.NoError(t, addAscRepoDefaults(x))
	_, err := x.Exec("INSERT INTO `asc_repo_defaults` (`org_id`) VALUES (3)")
	require.NoError(t, err)
	var row struct {
		RepoCreators   string
		BasePermission int
		Visibility     string
	}
	has, err := x.Table("asc_repo_defaults").Where("org_id = 3").Get(&row)
	require.NoError(t, err)
	assert.True(t, has)
	assert.Equal(t, "teams", row.RepoCreators)
	assert.Zero(t, row.BasePermission)
	assert.Empty(t, row.Visibility)
	_, err = x.Exec("INSERT INTO `asc_repo_defaults` (`org_id`) VALUES (3)")
	assert.Error(t, err, "one row for each organization")
}
