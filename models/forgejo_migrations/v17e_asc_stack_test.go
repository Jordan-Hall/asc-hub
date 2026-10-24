// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package forgejo_migrations

import (
	"testing"

	migration_tests "forgejo.org/models/gitea_migrations/test"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_addPullRequestAscStack(t *testing.T) {
	type PullRequest struct {
		ID int64 `xorm:"pk autoincr"`
	}
	x, deferable := migration_tests.PrepareTestEnv(t, 0, new(PullRequest))
	defer deferable()
	if x == nil || t.Failed() {
		return
	}

	require.NoError(t, addPullRequestAscStack(x))
	// A second run is harmless.
	require.NoError(t, addPullRequestAscStack(x))

	_, err := x.Exec("INSERT INTO `pull_request_asc_stack` (`pull_id`, `repo_id`, `asc_change`, `stack_parent_id`) VALUES (?, ?, ?, ?)",
		1, 1, "kkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkk", 0)
	require.NoError(t, err)
	// A pull request has one change.
	_, err = x.Exec("INSERT INTO `pull_request_asc_stack` (`pull_id`, `repo_id`, `asc_change`, `stack_parent_id`) VALUES (?, ?, ?, ?)",
		1, 1, "llllllllllllllllllllllllllllllll", 0)
	assert.Error(t, err)
}
