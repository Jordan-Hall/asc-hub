// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package forgejo_migrations

import (
	"testing"

	migration_tests "forgejo.org/models/gitea_migrations/test"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_addAccessTokenExpiryAndAscScopes(t *testing.T) {
	type AccessToken struct {
		ID    int64  `xorm:"pk autoincr"`
		Scope string `xorm:"TEXT"`
	}
	x, deferable := migration_tests.PrepareTestEnv(t, 0, new(AccessToken))
	defer deferable()
	if x == nil || t.Failed() {
		return
	}
	for _, s := range []string{"write:repository,read:user", "read:repository", "all", "read:user"} {
		_, err := x.Exec("INSERT INTO `access_token` (`scope`) VALUES (?)", s)
		require.NoError(t, err)
	}

	// The migration can run twice: a repeated run changes nothing.
	require.NoError(t, addAccessTokenExpiryAndAscScopes(x))
	require.NoError(t, addAccessTokenExpiryAndAscScopes(x))

	var scopes []string
	require.NoError(t, x.Table("access_token").Cols("scope").OrderBy("id").Find(&scopes))
	assert.Equal(t, []string{
		"write:repository,read:user,write:changes,write:runs",
		"read:repository,read:changes,read:runs",
		"all",
		"read:user",
	}, scopes)

	// no token expires unless it is told to
	var expires []int64
	require.NoError(t, x.Table("access_token").Cols("expires_unix").Find(&expires))
	for _, e := range expires {
		assert.Zero(t, e)
	}
}
