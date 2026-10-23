// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package forgejo_migrations

import (
	"testing"

	migration_tests "forgejo.org/models/gitea_migrations/test"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_addProtectedBranchAscRelaxedApprovals(t *testing.T) {
	type ProtectedBranch struct {
		ID       int64 `xorm:"pk autoincr"`
		RepoID   int64
		RuleName string `xorm:"'branch_name'"`
	}
	x, deferable := migration_tests.PrepareTestEnv(t, 0, new(ProtectedBranch))
	defer deferable()
	if x == nil || t.Failed() {
		return
	}

	_, err := x.Insert(&ProtectedBranch{RepoID: 1, RuleName: "main"})
	require.NoError(t, err)

	require.NoError(t, addProtectedBranchAscRelaxedApprovals(x))
	// A second run is harmless.
	require.NoError(t, addProtectedBranchAscRelaxedApprovals(x))

	var rows []map[string]string
	require.NoError(t, x.Table("protected_branch").Select("`branch_name`, `asc_relaxed_approvals`").Find(&rows))
	assert.Equal(t, []map[string]string{{"branch_name": "main", "asc_relaxed_approvals": "0"}}, rows)

	// The old row has no classes; a row written with the new column reads back.
	_, err = x.Exec("UPDATE `protected_branch` SET `asc_relaxed_classes` = ? WHERE `branch_name` = ?", `["docs"]`, "main")
	require.NoError(t, err)
	var classes string
	has, err := x.Table("protected_branch").Select("`asc_relaxed_classes`").Where("`branch_name` = ?", "main").Get(&classes)
	require.NoError(t, err)
	require.True(t, has)
	assert.JSONEq(t, `["docs"]`, classes)
}
