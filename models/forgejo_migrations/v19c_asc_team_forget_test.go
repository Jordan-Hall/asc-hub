// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package forgejo_migrations

import (
	"testing"

	migration_tests "forgejo.org/models/gitea_migrations/test"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_addAscTeamForget(t *testing.T) {
	x, deferable := migration_tests.PrepareTestEnv(t, 0)
	defer deferable()
	if x == nil || t.Failed() {
		return
	}
	require.NoError(t, addAscTeamForget(x))
	require.NoError(t, addAscTeamForget(x))
	_, err := x.Exec("INSERT INTO `asc_team_forget` (`org_name`, `created_unix`) VALUES ('oldorg', 1)")
	require.NoError(t, err)
	_, err = x.Exec("INSERT INTO `asc_team_forget` (`org_name`, `created_unix`) VALUES ('oldorg', 2)")
	require.Error(t, err, "a name is kept once")
	n, err := x.Table("asc_team_forget").Count()
	require.NoError(t, err)
	assert.EqualValues(t, 1, n)
}
