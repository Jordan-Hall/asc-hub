// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package forgejo_migrations

import (
	"testing"

	migration_tests "forgejo.org/models/gitea_migrations/test"

	"github.com/stretchr/testify/require"
)

func Test_addAscRepoFeatureOff(t *testing.T) {
	x, deferable := migration_tests.PrepareTestEnv(t, 0)
	defer deferable()
	if x == nil || t.Failed() {
		return
	}
	require.NoError(t, addAscRepoFeatureOff(x))
	require.NoError(t, addAscRepoFeatureOff(x))

	_, err := x.Exec("INSERT INTO `asc_repo_feature_off` (`repo_id`, `feature`) VALUES (1, 'agents')")
	require.NoError(t, err)
	_, err = x.Exec("INSERT INTO `asc_repo_feature_off` (`repo_id`, `feature`) VALUES (1, 'agents')")
	require.Error(t, err, "one row per repository and switch")
}
