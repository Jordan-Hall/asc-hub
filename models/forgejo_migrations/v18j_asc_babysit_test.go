// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package forgejo_migrations

import (
	"testing"

	migration_tests "forgejo.org/models/gitea_migrations/test"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_addAscBabysit(t *testing.T) {
	x, deferable := migration_tests.PrepareTestEnv(t, 0)
	defer deferable()
	if x == nil || t.Failed() {
		return
	}
	require.NoError(t, addAscBabysit(x))
	require.NoError(t, addAscBabysit(x))
	_, err := x.Exec("INSERT INTO `asc_babysit` (`pull_id`, `repo_id`, `agent`, `doer_id`, `created_unix`, `updated_unix`) VALUES (7, 1, 'coder', 2, 1, 1)")
	require.NoError(t, err)
	_, err = x.Exec("INSERT INTO `asc_babysit` (`pull_id`, `repo_id`, `agent`, `doer_id`, `created_unix`, `updated_unix`) VALUES (7, 1, 'fixer', 2, 1, 1)")
	require.Error(t, err, "one watch for a change")
	var runs int
	_, err = x.SQL("SELECT `runs` FROM `asc_babysit` WHERE `pull_id` = 7").Get(&runs)
	require.NoError(t, err)
	assert.Equal(t, 0, runs)
}
