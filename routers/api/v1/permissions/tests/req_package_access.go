// Copyright 2026 The Forgejo Authors.
// SPDX-License-Identifier: GPLv3-or-later

package tests

import (
	"testing"

	"forgejo.org/models/perm"
	apiv1_permissions "forgejo.org/routers/api/v1/permissions"
)

var _ = registerFunctionTestBuilder([]string{"ReqPackageAccess "}, func(_ *testing.T, signatureString string, signature []any) {
	signatureStringToFunctionTest[signatureString] = functionTest{
		sequenceFilter: []string{
			"APIAuthorization",
			signatureString,
		},
		fulfillNeeds: func(t *testing.T, data *fixtureData) {
			t.Helper()
			data.SetDefault("doer", "doerregular")
			if data.Get("packageOwner") == "doer" {
				data.Set("packageOwner", data.Get("doer"))
			}
			data.SetDefault("packageOwner", data.Get("doer"))
		},
		interpret: func(t *testing.T, permissions *apiv1_permissions.Permissions, data *fixtureData) {
			fixtureSetPackageOwner(t, permissions, data)
		},
		fixtures: []*fixtureType{
			{
				data: newFixtureData(map[string]string{
					"packageOwner": "doer",
					"doer":         "doeradmin",
				}),
			},
			{
				data: newFixtureData(map[string]string{
					"doer":         "userregular",
					"packageOwner": "userprivate",
				}),
				error: "user should have specific permission or be a site admin",
			},
		},
		staticArgs: 1,
		call: func(t *testing.T, ctx apiv1_permissions.Context, _ *fixtureData, args []any) {
			mode := args[0].(perm.AccessMode)
			t.Logf("calling ReqPackageAccess(ctx, %s)", mode)
			apiv1_permissions.ReqPackageAccess(ctx, mode)
		},
	}
})
