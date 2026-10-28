// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package forgejo_migrations

import (
	"testing"

	migration_tests "forgejo.org/models/gitea_migrations/test"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_addAscOrgDomain(t *testing.T) {
	type User struct {
		ID int64 `xorm:"pk autoincr"`
	}
	x, deferable := migration_tests.PrepareTestEnv(t, 0, new(User))
	defer deferable()
	if x == nil || t.Failed() {
		return
	}
	require.NoError(t, addAscOrgDomain(x))
	require.NoError(t, addAscOrgDomain(x))
	_, err := x.Exec("INSERT INTO `asc_org_domain` (`org_id`, `domain`, `token`) VALUES (3, 'acme.dev', 't')")
	require.NoError(t, err)
	_, err = x.Exec("INSERT INTO `asc_org_domain` (`org_id`, `domain`, `token`) VALUES (3, 'other.dev', 't')")
	assert.Error(t, err, "one claim for each organization")
}
