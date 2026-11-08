// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package forgejo_migrations

import (
	"testing"

	migration_tests "forgejo.org/models/gitea_migrations/test"

	"github.com/stretchr/testify/require"
)

func Test_addAscRemovedRule(t *testing.T) {
	x, deferable := migration_tests.PrepareTestEnv(t, 0)
	defer deferable()
	if x == nil || t.Failed() {
		return
	}
	require.NoError(t, addAscRemovedRule(x))
	require.NoError(t, addAscRemovedRule(x))

	_, err := x.Exec("INSERT INTO `asc_removed_rule` (`repo_id`, `rule_name`, `rule`, `removed_unix`) VALUES (1, 'main', '{}', 1)")
	require.NoError(t, err)
	_, err = x.Exec("INSERT INTO `asc_removed_rule` (`repo_id`, `rule_name`, `rule`, `removed_unix`) VALUES (1, 'main', '{}', 2)")
	require.NoError(t, err, "a name can be removed again after it was put back")
}
