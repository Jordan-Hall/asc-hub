// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package org_test

import (
	"net/http"
	"testing"

	"forgejo.org/models/db"
	"forgejo.org/models/organization"
	"forgejo.org/models/perm"
	"forgejo.org/models/unittest"
	"forgejo.org/modules/web"
	"forgejo.org/routers/web/org"
	"forgejo.org/services/contexttest"
	"forgejo.org/services/forms"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// postOrgSettings posts the settings form of org3 as its owner, with the fields org3 already has.
func postOrgSettings(t *testing.T, creators, base, visibility string) (int, map[string]any) {
	t.Helper()
	ctx, resp := contexttest.MockContext(t, "org/org3/settings")
	contexttest.LoadUser(t, ctx, 2)
	contexttest.LoadOrganization(t, ctx, 3)
	ctx.ContextUser = ctx.Org.Organization.AsUser()
	ctx.Org.IsOwner = true
	o := ctx.Org.Organization
	web.SetForm(ctx, &forms.UpdateOrgSettingForm{
		Name:            o.Name,
		FullName:        o.FullName,
		Email:           o.Email,
		Visibility:      o.Visibility,
		MaxRepoCreation: -1,
		RepoCreators:    creators,
		BasePermission:  base,
		RepoVisibility:  visibility,
	})
	org.SettingsPost(ctx)
	return resp.Code, ctx.Data
}

func repoDefaults(t *testing.T) *organization.AscRepoDefaults {
	t.Helper()
	d, err := organization.GetAscRepoDefaults(db.DefaultContext, 3)
	require.NoError(t, err)
	return d
}

func TestOrgSettingsRepoDefaults(t *testing.T) {
	unittest.PrepareTestEnv(t)

	code, _ := postOrgSettings(t, "maintainers", "read", "internal")
	require.Equal(t, http.StatusSeeOther, code)
	d := repoDefaults(t)
	assert.Equal(t, organization.RepoCreatorsMaintainers, d.RepoCreators)
	assert.Equal(t, perm.AccessModeRead, d.BasePermission)
	assert.Equal(t, organization.RepoVisibilityInternal, d.Visibility)

	t.Run("a value outside the lists saves nothing", func(t *testing.T) {
		for _, bad := range [][3]string{{"everyone", "read", ""}, {"teams", "admin", ""}, {"teams", "none", "secret"}} {
			code, data := postOrgSettings(t, bad[0], bad[1], bad[2])
			assert.Equal(t, http.StatusOK, code, bad)
			assert.Equal(t, true, data["Err_RepoDefaults"], bad)
		}
		d := repoDefaults(t)
		assert.Equal(t, organization.RepoCreatorsMaintainers, d.RepoCreators)
	})

	t.Run("the defaults remove the row", func(t *testing.T) {
		code, _ := postOrgSettings(t, "teams", "none", "")
		require.Equal(t, http.StatusSeeOther, code)
		has, err := db.GetEngine(db.DefaultContext).Exist(&organization.AscRepoDefaults{OrgID: 3})
		require.NoError(t, err)
		assert.False(t, has)
	})
}
