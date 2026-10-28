// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package forgejo_migrations

import (
	"testing"

	migration_tests "forgejo.org/models/gitea_migrations/test"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_addAscProfilePin(t *testing.T) {
	type User struct {
		ID int64 `xorm:"pk autoincr"`
	}
	x, deferable := migration_tests.PrepareTestEnv(t, 0, new(User))
	defer deferable()
	if x == nil || t.Failed() {
		return
	}
	require.NoError(t, addAscProfilePin(x))
	require.NoError(t, addAscProfilePin(x))

	_, err := x.Exec("INSERT INTO `asc_profile_pin` (`user_id`, `repo_id`, `position`) VALUES (1, 5, 0)")
	require.NoError(t, err)
	// a repository is pinned once by a person, and another person can pin it too
	_, err = x.Exec("INSERT INTO `asc_profile_pin` (`user_id`, `repo_id`, `position`) VALUES (1, 5, 1)")
	assert.Error(t, err)
	_, err = x.Exec("INSERT INTO `asc_profile_pin` (`user_id`, `repo_id`, `position`) VALUES (2, 5, 0)")
	require.NoError(t, err)
}
