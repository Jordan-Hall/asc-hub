// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package forgejo_migrations

import (
	"testing"

	migration_tests "forgejo.org/models/gitea_migrations/test"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_addAscTrendingRepo(t *testing.T) {
	type Repository struct {
		ID int64 `xorm:"pk autoincr"`
	}
	x, deferable := migration_tests.PrepareTestEnv(t, 0, new(Repository))
	defer deferable()
	if x == nil || t.Failed() {
		return
	}
	require.NoError(t, addAscTrendingRepo(x))
	require.NoError(t, addAscTrendingRepo(x))
	_, err := x.Exec("INSERT INTO `asc_trending_repo` (`repo_id`, `score`) VALUES (1, 10)")
	require.NoError(t, err)
	_, err = x.Exec("INSERT INTO `asc_trending_repo` (`repo_id`, `score`) VALUES (1, 11)")
	assert.Error(t, err, "one row for each repository")
}
