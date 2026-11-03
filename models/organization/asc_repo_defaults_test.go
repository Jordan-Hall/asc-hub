// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package organization_test

import (
	"testing"

	"forgejo.org/models/db"
	"forgejo.org/models/organization"
	"forgejo.org/models/perm"
	"forgejo.org/models/unittest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setRepoCreators(t *testing.T, orgID int64, who string) {
	t.Helper()
	require.NoError(t, organization.SetAscRepoDefaults(db.DefaultContext, &organization.AscRepoDefaults{OrgID: orgID, RepoCreators: who}))
}

func canCreate(t *testing.T, orgID, uid int64) bool {
	t.Helper()
	ok, err := organization.CanCreateOrgRepo(db.DefaultContext, orgID, uid)
	require.NoError(t, err)
	return ok
}

func TestAscRepoCreators(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	ctx := db.DefaultContext
	// Org 3: user 2 is an owner, user 28 is in team 12 (admin, may create), user 4 is in team 1 (write, may not).
	// Team 14 (write, may create) gets user 4 as well, and team 12 stops allowing it, so each rule differs.
	unittest.AssertSuccessfulInsert(t, &organization.TeamUser{OrgID: 3, TeamID: 14, UID: 4})
	_, err := db.GetEngine(ctx).ID(12).Cols("can_create_org_repo").Update(&organization.Team{CanCreateOrgRepo: false})
	require.NoError(t, err)

	cases := []struct {
		who            string
		owner, admin   bool
		writerAllowed  bool
		creatorsInOrg3 int
	}{
		{organization.RepoCreatorsTeams, true, false, true, 2},
		{organization.RepoCreatorsMaintainers, true, true, false, 2},
		{organization.RepoCreatorsOwners, true, false, false, 1},
	}
	for _, c := range cases {
		t.Run(c.who, func(t *testing.T) {
			setRepoCreators(t, 3, c.who)
			assert.Equal(t, c.owner, canCreate(t, 3, 2), "owner")
			assert.Equal(t, c.admin, canCreate(t, 3, 28), "admin team member")
			assert.Equal(t, c.writerAllowed, canCreate(t, 3, 4), "write team that allows it")

			orgs, err := organization.GetOrgsCanCreateRepoByUserID(ctx, 4)
			require.NoError(t, err)
			assert.Equal(t, c.writerAllowed, len(orgs) == 1, "orgs user 4 may create in")

			users, err := organization.GetUsersWhoCanCreateOrgRepo(ctx, 3)
			require.NoError(t, err)
			assert.Len(t, users, c.creatorsInOrg3)
		})
	}
	// Another organization keeps its own rule: user 1 creates in orgs 35 and 36 whatever org 3 says.
	orgs, err := organization.GetOrgsCanCreateRepoByUserID(ctx, 1)
	require.NoError(t, err)
	assert.Len(t, orgs, 2)
}

func TestAscRepoDefaultsStore(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	ctx := db.DefaultContext

	d, err := organization.GetAscRepoDefaults(ctx, 3)
	require.NoError(t, err)
	assert.Equal(t, organization.RepoCreatorsTeams, d.RepoCreators)
	assert.Equal(t, perm.AccessModeNone, d.BasePermission)
	assert.Empty(t, d.Visibility)

	want := &organization.AscRepoDefaults{OrgID: 3, RepoCreators: organization.RepoCreatorsOwners, BasePermission: perm.AccessModeRead, Visibility: organization.RepoVisibilityInternal}
	require.NoError(t, organization.SetAscRepoDefaults(ctx, want))
	d, err = organization.GetAscRepoDefaults(ctx, 3)
	require.NoError(t, err)
	assert.Equal(t, []any{want.RepoCreators, want.BasePermission, want.Visibility}, []any{d.RepoCreators, d.BasePermission, d.Visibility})

	vis, err := organization.GetAscRepoVisibilities(ctx, []int64{3, 6, 2})
	require.NoError(t, err)
	assert.Equal(t, map[int64]string{3: "internal"}, vis)

	base, err := organization.GetAscBasePermission(ctx, 3, 4)
	require.NoError(t, err)
	assert.Equal(t, perm.AccessModeRead, base)
	base, err = organization.GetAscBasePermission(ctx, 3, 5)
	require.NoError(t, err)
	assert.Equal(t, perm.AccessModeNone, base, "user 5 is not a member of org 3")

	// Setting the defaults again removes the row.
	require.NoError(t, organization.SetAscRepoDefaults(ctx, &organization.AscRepoDefaults{OrgID: 3, RepoCreators: organization.RepoCreatorsTeams}))
	assertNoRepoDefaults(t, 3)

	for _, bad := range []*organization.AscRepoDefaults{
		{OrgID: 3, RepoCreators: "everyone"},
		{OrgID: 3, RepoCreators: organization.RepoCreatorsTeams, BasePermission: perm.AccessModeAdmin},
		{OrgID: 3, RepoCreators: organization.RepoCreatorsTeams, Visibility: "secret"},
	} {
		require.Error(t, organization.SetAscRepoDefaults(ctx, bad))
	}
	assertNoRepoDefaults(t, 3)
}

func TestDeleteOrganizationDropsRepoDefaults(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	ctx := db.DefaultContext
	// Org 6 has no repositories, so it can be deleted.
	require.NoError(t, organization.SetAscRepoDefaults(ctx, &organization.AscRepoDefaults{OrgID: 6, RepoCreators: organization.RepoCreatorsOwners}))
	org := unittest.AssertExistsAndLoadBean(t, &organization.Organization{ID: 6})
	require.NoError(t, organization.DeleteOrganization(ctx, org))
	assertNoRepoDefaults(t, 6)
}

// assertNoRepoDefaults checks that the organization has no row; the table is keyed by org_id, not id.
func assertNoRepoDefaults(t *testing.T, orgID int64) {
	t.Helper()
	has, err := db.GetEngine(db.DefaultContext).Exist(&organization.AscRepoDefaults{OrgID: orgID})
	require.NoError(t, err)
	assert.False(t, has)
}
