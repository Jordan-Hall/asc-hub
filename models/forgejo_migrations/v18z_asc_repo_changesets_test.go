// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package forgejo_migrations

import (
	"testing"

	migration_tests "forgejo.org/models/gitea_migrations/test"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_addAscRepoChangesets(t *testing.T) {
	x, deferable := migration_tests.PrepareTestEnv(t, 0)
	defer deferable()
	if x == nil || t.Failed() {
		return
	}
	require.NoError(t, addAscRepoChangesets(x))
	require.NoError(t, addAscRepoChangesets(x), "running twice changes nothing")
	_, err := x.Exec("INSERT INTO `asc_repo_changesets` (`repo_id`, `commit_sha`, `changesets`) VALUES (1, 'abc', 3812)")
	require.NoError(t, err)
	var row struct {
		CommitSha  string
		Changesets int64
	}
	has, err := x.Table("asc_repo_changesets").Where("repo_id = 1").Get(&row)
	require.NoError(t, err)
	assert.True(t, has)
	assert.Equal(t, "abc", row.CommitSha)
	assert.EqualValues(t, 3812, row.Changesets)
	_, err = x.Exec("INSERT INTO `asc_repo_changesets` (`repo_id`) VALUES (1)")
	assert.Error(t, err, "one count per repository")
}
