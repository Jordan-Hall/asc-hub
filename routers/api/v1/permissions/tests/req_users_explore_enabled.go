// Copyright 2026 The Forgejo Authors.
// SPDX-License-Identifier: GPLv3-or-later

package tests

import (
	"testing"

	"forgejo.org/modules/setting"
	apiv1_permissions "forgejo.org/routers/api/v1/permissions"
)

var _ = registerFunctionTest(apiv1_permissions.ReqUsersExploreEnabled, functionTest{
	protectSettingsBool: []*bool{
		&setting.Service.Explore.DisableUsersPage,
	},
	interpret: func(t *testing.T, permissions *apiv1_permissions.Permissions, data *fixtureData) {
		setting.Service.Explore.DisableUsersPage = data.Get("Service.Explore.DisableUsersPage") == "true"
	},
	fixtures: []*fixtureType{
		{
			data: newFixtureData(map[string]string{}),
		},
		{
			data: newFixtureData(map[string]string{
				"Service.Explore.DisableUsersPage": "true",
			}),
			error: "Not Found",
		},
	},
})
