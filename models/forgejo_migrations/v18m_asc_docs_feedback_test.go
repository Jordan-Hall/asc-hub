// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package forgejo_migrations

import (
	"testing"

	migration_tests "forgejo.org/models/gitea_migrations/test"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_addAscDocsFeedback(t *testing.T) {
	x, deferable := migration_tests.PrepareTestEnv(t, 0)
	defer deferable()
	if x == nil || t.Failed() {
		return
	}
	require.NoError(t, addAscDocsFeedback(x))
	require.NoError(t, addAscDocsFeedback(x))
	_, err := x.Exec("INSERT INTO `asc_docs_feedback` (`slug`, `helpful`, `created_unix`) VALUES ('install', true, 1)")
	require.NoError(t, err)
	n, err := x.Table("asc_docs_feedback").Count()
	require.NoError(t, err)
	assert.EqualValues(t, 1, n)
}
