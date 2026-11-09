// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package forgejo_migrations

import (
	"testing"

	migration_tests "forgejo.org/models/gitea_migrations/test"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_addPullRequestAscDirs(t *testing.T) {
	x, deferable := migration_tests.PrepareTestEnv(t, 0)
	defer deferable()
	if x == nil || t.Failed() {
		return
	}
	require.NoError(t, addPullRequestAscDirs(x))
	require.NoError(t, addPullRequestAscDirs(x))
	_, err := x.Exec("INSERT INTO `pull_request_asc_dirs` (`pull_id`, `head`, `dirs`) VALUES (1, 'abc', '{\"src/\":2}')")
	require.NoError(t, err)
	n, err := x.Table("pull_request_asc_dirs").Count()
	require.NoError(t, err)
	assert.EqualValues(t, 1, n)
}
