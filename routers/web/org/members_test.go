// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package org_test

import (
	"testing"

	"forgejo.org/models/unittest"
	"forgejo.org/routers/web/org"
	"forgejo.org/services/contexttest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// membersPage runs the People page of org3 as the given user (0 is anonymous) and returns the page data.
func membersPage(t *testing.T, doerID int64, query string) map[string]any {
	t.Helper()
	ctx, _ := contexttest.MockContext(t, "org3/members"+query)
	contexttest.LoadOrganization(t, ctx, 3)
	ctx.ContextUser = ctx.Org.Organization.AsUser()
	if doerID != 0 {
		contexttest.LoadUser(t, ctx, doerID)
		owner, err := ctx.Org.Organization.IsOwnedBy(ctx, doerID)
		require.NoError(t, err)
		ctx.Org.IsOwner = owner
	}
	org.Members(ctx)
	return ctx.Data
}

func TestMembersTwoFactorStatusIsForOwners(t *testing.T) {
	unittest.PrepareTestEnv(t)

	// user2 owns org3, user4 is a plain member of it
	for name, doer := range map[string]int64{"anonymous": 0, "member": 4} {
		t.Run(name, func(t *testing.T) {
			data := membersPage(t, doer, "?filter=2fa_off")
			assert.Equal(t, false, data["CanSee2FA"])
			assert.NotContains(t, data, "PeopleCountTwoFAOff")
			assert.NotContains(t, data, "MembersTwoFaStatus")
			assert.Empty(t, data["PeopleFilter"], "the filter by 2FA is ignored, not applied")
		})
	}

	t.Run("owner", func(t *testing.T) {
		data := membersPage(t, 2, "?filter=2fa_off")
		assert.Equal(t, true, data["CanSee2FA"])
		assert.Contains(t, data, "PeopleCountTwoFAOff")
		assert.Contains(t, data, "MembersTwoFaStatus")
		assert.Equal(t, "2fa_off", data["PeopleFilter"])
	})
}
