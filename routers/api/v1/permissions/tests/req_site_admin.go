// Copyright 2026 The Forgejo Authors.
// SPDX-License-Identifier: GPLv3-or-later

package tests

import (
	"testing"

	apiv1_permissions "forgejo.org/routers/api/v1/permissions"
)

var _ = registerFunctionTest(apiv1_permissions.ReqSiteAdmin, functionTest{
	fulfillNeeds: func(t *testing.T, data *fixtureData) {
		t.Helper()
		data.SetDefault("doer", "doeradmin")
	},
	fixtures: []*fixtureType{
		{
			data: newFixtureData(map[string]string{
				"doer": "doeradmin",
			}),
		},
		{
			data: newFixtureData(map[string]string{
				"doer": "regularuser",
			}),
			error: "user should be the site admin",
		},
	},
})
