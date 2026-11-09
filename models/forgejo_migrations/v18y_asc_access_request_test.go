// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package forgejo_migrations

import (
	"testing"

	migration_tests "forgejo.org/models/gitea_migrations/test"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_addAscAccessRequest(t *testing.T) {
	x, deferable := migration_tests.PrepareTestEnv(t, 0)
	defer deferable()
	if x == nil || t.Failed() {
		return
	}
	require.NoError(t, addAscAccessRequest(x))
	require.NoError(t, addAscAccessRequest(x), "running twice changes nothing")
	_, err := x.Exec("INSERT INTO `asc_access_request` (`repo_id`, `user_id`, `message`) VALUES (1, 5, 'please')")
	require.NoError(t, err)
	var row struct {
		Mode    int
		Message string
	}
	has, err := x.Table("asc_access_request").Where("repo_id = 1 AND user_id = 5").Get(&row)
	require.NoError(t, err)
	assert.True(t, has)
	assert.Equal(t, 2, row.Mode, "a request asks for write access unless it says otherwise")
	assert.Equal(t, "please", row.Message)
	_, err = x.Exec("INSERT INTO `asc_access_request` (`repo_id`, `user_id`) VALUES (1, 5)")
	assert.Error(t, err, "a person has one request per repository")
}
