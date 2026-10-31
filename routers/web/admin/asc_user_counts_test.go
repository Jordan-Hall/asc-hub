// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package admin

import (
	"testing"

	"forgejo.org/models/db"
	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/optional"
	"forgejo.org/services/contexttest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAdminUserCountsMatchTheFilters(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	ctx, _ := contexttest.MockContext(t, "admin/users")
	contexttest.LoadUser(t, ctx, 1) // an administrator in the fixtures
	ctx.IsSigned = true

	c := adminUserCounts(ctx)
	count := func(f func(o *user_model.SearchUserOptions)) int64 {
		o := &user_model.SearchUserOptions{Actor: ctx.Doer, Type: user_model.UserTypeIndividual, ListOptions: db.ListOptions{PageSize: 1, Page: 1}, IncludeReserved: true}
		f(o)
		_, n, err := user_model.SearchUsers(ctx, o)
		require.NoError(t, err)
		return n
	}
	assert.Equal(t, count(func(*user_model.SearchUserOptions) {}), c.All)
	assert.Equal(t, count(func(o *user_model.SearchUserOptions) { o.IsAdmin = optional.Some(true) }), c.Admins)
	assert.Equal(t, count(func(o *user_model.SearchUserOptions) { o.IsActive = optional.Some(false) }), c.Inactive)
	assert.GreaterOrEqual(t, c.Admins, int64(1), "the fixtures have an administrator")
	assert.Greater(t, c.All, c.Admins)
	for name, n := range map[string]int64{"2fa off": c.TwoFAOff, "inactive": c.Inactive, "suspended": c.Suspended, "restricted": c.Restricted, "admins": c.Admins} {
		assert.LessOrEqual(t, n, c.All, name)
	}
}
