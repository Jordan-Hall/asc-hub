// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package forgejo_migrations

import (
	"testing"

	migration_tests "forgejo.org/models/gitea_migrations/test"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_addPullRequestAscRun(t *testing.T) {
	type PullRequest struct {
		ID int64 `xorm:"pk autoincr"`
	}
	x, deferable := migration_tests.PrepareTestEnv(t, 0, new(PullRequest))
	defer deferable()
	if x == nil || t.Failed() {
		return
	}

	require.NoError(t, addPullRequestAscRun(x))
	require.NoError(t, addPullRequestAscRun(x), "the migration can run twice")

	const insert = "INSERT INTO `pull_request_asc_run` (`pull_id`, `repo_id`, `run_id`, `agent`, `started_by`) VALUES (?, ?, ?, ?, ?)"
	_, err := x.Exec(insert, 1, 1, 7, "coder-2", "alice")
	require.NoError(t, err)
	_, err = x.Exec(insert, 1, 1, 8, "coder-2", "alice")
	assert.Error(t, err, "a pull request belongs to one run")
	_, err = x.Exec(insert, 2, 1, 7, "coder-2", "alice")
	require.NoError(t, err, "a run has many pull requests")
}
