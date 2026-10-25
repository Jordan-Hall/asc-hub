// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package forgejo_migrations

import (
	"testing"

	migration_tests "forgejo.org/models/gitea_migrations/test"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_addAscStackQueue(t *testing.T) {
	type PullRequest struct {
		ID int64 `xorm:"pk autoincr"`
	}
	x, deferable := migration_tests.PrepareTestEnv(t, 0, new(PullRequest))
	defer deferable()
	if x == nil || t.Failed() {
		return
	}

	require.NoError(t, addAscStackQueue(x))
	require.NoError(t, addAscStackQueue(x))

	ins := "INSERT INTO `asc_stack_queue` (`repo_id`, `branch`, `pull_id`, `asc_change`, `doer_id`, `state`, `created_unix`, `updated_unix`) VALUES (1, 'master', ?, 'kkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkk', 2, 'queued', 0, 0)"
	_, err := x.Exec(ins, 1)
	require.NoError(t, err)
	// A pull request is queued once.
	_, err = x.Exec(ins, 1)
	assert.Error(t, err)

	// A branch has one lease row.
	_, err = x.Exec("INSERT INTO `asc_stack_queue_branch` (`repo_id`, `branch`) VALUES (1, 'master')")
	require.NoError(t, err)
	_, err = x.Exec("INSERT INTO `asc_stack_queue_branch` (`repo_id`, `branch`) VALUES (1, 'master')")
	assert.Error(t, err)
}
