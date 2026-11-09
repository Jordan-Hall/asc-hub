// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package forgejo_migrations

import (
	"testing"

	migration_tests "forgejo.org/models/gitea_migrations/test"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_addAscTeamAgent(t *testing.T) {
	x, deferable := migration_tests.PrepareTestEnv(t, 0)
	defer deferable()
	if x == nil || t.Failed() {
		return
	}
	require.NoError(t, addAscTeamAgent(x))
	require.NoError(t, addAscTeamAgent(x))
	_, err := x.Exec("INSERT INTO `asc_team_agent` (`org_id`, `team_id`, `agent`, `created_unix`) VALUES (3, 1, 'bot', 1)")
	require.NoError(t, err)
	_, err = x.Exec("INSERT INTO `asc_team_agent` (`org_id`, `team_id`, `agent`, `created_unix`) VALUES (3, 1, 'bot', 2)")
	require.Error(t, err, "an agent is in a team once")
	_, err = x.Exec("INSERT INTO `asc_team_agent` (`org_id`, `team_id`, `agent`, `created_unix`) VALUES (3, 2, 'bot', 2)")
	require.NoError(t, err)
	n, err := x.Table("asc_team_agent").Count()
	require.NoError(t, err)
	assert.EqualValues(t, 2, n)
}
