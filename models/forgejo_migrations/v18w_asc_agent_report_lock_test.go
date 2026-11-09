// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package forgejo_migrations

import (
	"testing"

	migration_tests "forgejo.org/models/gitea_migrations/test"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_addAscAgentReportLock(t *testing.T) {
	x, deferable := migration_tests.PrepareTestEnv(t, 0)
	defer deferable()
	if x == nil || t.Failed() {
		return
	}
	require.NoError(t, addAscAgentReportLock(x))
	require.NoError(t, addAscAgentReportLock(x), "running twice changes nothing")
	_, err := x.Insert(&v18wAgentReportLock{Token: "at_a"})
	require.NoError(t, err)
	_, err = x.Insert(&v18wAgentReportLock{Token: "at_a"})
	require.Error(t, err, "one row per token")
	n, err := x.Count(new(v18wAgentReportLock))
	require.NoError(t, err)
	assert.EqualValues(t, 1, n)
}
