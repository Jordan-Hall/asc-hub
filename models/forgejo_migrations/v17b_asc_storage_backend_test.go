// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package forgejo_migrations

import (
	"testing"

	migration_tests "forgejo.org/models/gitea_migrations/test"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_addRepositoryStorageBackend(t *testing.T) {
	type Repository struct {
		ID        int64 `xorm:"pk autoincr"`
		OwnerName string
		Name      string
	}
	x, deferable := migration_tests.PrepareTestEnv(t, 0, new(Repository))
	defer deferable()
	if x == nil || t.Failed() {
		return
	}

	_, err := x.Insert(&Repository{OwnerName: "o", Name: "a"}, &Repository{OwnerName: "o", Name: "b"})
	require.NoError(t, err)

	require.NoError(t, addRepositoryStorageBackend(x))
	// A second run is harmless.
	require.NoError(t, addRepositoryStorageBackend(x))

	var rows []map[string]string
	require.NoError(t, x.Table("repository").Select("`name`, `storage_backend`").OrderBy("`id`").Find(&rows))
	assert.Equal(t, []map[string]string{
		{"name": "a", "storage_backend": "git"},
		{"name": "b", "storage_backend": "git"},
	}, rows)

	// The column defaults to git for rows written without it.
	_, err = x.Exec("INSERT INTO `repository` (`owner_name`, `name`) VALUES ('o', 'c')")
	require.NoError(t, err)
	var got string
	has, err := x.Table("repository").Select("`storage_backend`").Where("`name` = ?", "c").Get(&got)
	require.NoError(t, err)
	require.True(t, has)
	assert.Equal(t, "git", got)
}
