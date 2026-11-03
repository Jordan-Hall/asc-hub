// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package forgejo_migrations

import (
	"testing"

	migration_tests "forgejo.org/models/gitea_migrations/test"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_addAscExportWatch(t *testing.T) {
	x, deferable := migration_tests.PrepareTestEnv(t, 0)
	defer deferable()
	if x == nil || t.Failed() {
		return
	}
	require.NoError(t, addAscExportWatch(x))
	require.NoError(t, addAscExportWatch(x))
	_, err := x.Exec("INSERT INTO `asc_export_watch` (`user_id`, `export_id`, `created_unix`) VALUES (1, 'ex_aaaaaaaaaaaaaaaaaaaa', 1)")
	require.NoError(t, err)
	_, err = x.Exec("INSERT INTO `asc_export_watch` (`user_id`, `export_id`, `created_unix`) VALUES (1, 'ex_aaaaaaaaaaaaaaaaaaaa', 2)")
	require.Error(t, err, "one watch for an export of a person")
	n, err := x.Table("asc_export_watch").Count()
	require.NoError(t, err)
	assert.Equal(t, int64(1), n)
}
