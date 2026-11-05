// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package forgejo_migrations

import (
	"testing"

	migration_tests "forgejo.org/models/gitea_migrations/test"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_addAscTermsAcceptance(t *testing.T) {
	type User struct {
		ID int64 `xorm:"pk autoincr"`
	}
	x, deferable := migration_tests.PrepareTestEnv(t, 0, new(User))
	defer deferable()
	if x == nil || t.Failed() {
		return
	}
	require.NoError(t, addAscTermsAcceptance(x))
	require.NoError(t, addAscTermsAcceptance(x))

	_, err := x.Exec("INSERT INTO `asc_terms_acceptance` (`user_id`, `accepted_unix`) VALUES (1, 100)")
	require.NoError(t, err)
	// one record per person
	_, err = x.Exec("INSERT INTO `asc_terms_acceptance` (`user_id`, `accepted_unix`) VALUES (1, 200)")
	assert.Error(t, err)
	_, err = x.Exec("INSERT INTO `asc_terms_acceptance` (`user_id`, `accepted_unix`) VALUES (2, 100)")
	require.NoError(t, err)
}
