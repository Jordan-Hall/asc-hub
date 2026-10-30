// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package forgejo_migrations

import (
	"testing"

	migration_tests "forgejo.org/models/gitea_migrations/test"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_addRepoTrash(t *testing.T) {
	type Repository struct {
		ID int64 `xorm:"pk autoincr"`
	}
	x, deferable := migration_tests.PrepareTestEnv(t, 0, new(Repository))
	defer deferable()
	if x == nil || t.Failed() {
		return
	}
	require.NoError(t, addRepoTrash(x))
	require.NoError(t, addRepoTrash(x), "the migration can run twice")
	type RepoTrash struct {
		RepoID    int64
		PurgeUnix int64
	}
	_, err := x.Insert(&RepoTrash{RepoID: 1, PurgeUnix: 5})
	require.NoError(t, err)
	var got []RepoTrash
	require.NoError(t, x.Table("repo_trash").Find(&got))
	assert.Len(t, got, 1)
	_, err = x.Insert(&RepoTrash{RepoID: 1, PurgeUnix: 6})
	require.Error(t, err, "a repository is in the trash once")
}
