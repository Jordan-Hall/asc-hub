// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package forgejo_migrations

import (
	"testing"

	migration_tests "forgejo.org/models/gitea_migrations/test"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_addRepositoryIsInternal(t *testing.T) {
	type Repository struct {
		ID        int64 `xorm:"pk autoincr"`
		IsPrivate bool
	}
	x, deferable := migration_tests.PrepareTestEnv(t, 0, new(Repository))
	defer deferable()
	if x == nil || t.Failed() {
		return
	}
	_, err := x.Insert(&Repository{IsPrivate: true})
	require.NoError(t, err)
	require.NoError(t, addRepositoryIsInternal(x))
	require.NoError(t, addRepositoryIsInternal(x), "the migration can run twice")
	type Row struct {
		ID         int64
		IsInternal bool
	}
	var rows []Row
	require.NoError(t, x.Table("repository").Find(&rows))
	require.Len(t, rows, 1)
	assert.False(t, rows[0].IsInternal, "existing repositories are not internal")
}
