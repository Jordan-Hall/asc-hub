// Copyright 2026 The Forgejo Authors.
// SPDX-License-Identifier: GPLv3-or-later

package tests_test

import (
	"strings"
	"testing"

	unit_model "forgejo.org/models/unit"
	apiv1_permissions "forgejo.org/routers/api/v1/permissions"

	"github.com/stretchr/testify/require"
)

var _ = registerFunctionTestBuilder([]string{"ReqRepoWriter "}, func(t *testing.T, signatureString string, signature []any) {
	t.Helper()
	unitTypes := signature[1].([]unit_model.Type)
	units := unitsTypeToString(unitTypes...)
	scopes := unitsToScopes(unitTypes, "write")
	signatureStringToFunctionTest[signatureString] = functionTest{
		sequenceFilter: []string{
			"APIAuthorization",
			"RepoAccess",
			signatureString,
		},
		interpret: func(t *testing.T, permissions *apiv1_permissions.Permissions, data *fixtureData) {
			fixtureDisableUnits(t, permissions, data)
		},
		fulfillNeeds: func(t *testing.T, data *fixtureData) {
			t.Helper()
			if data.Has("repository") {
				owner, _, found := strings.Cut(data.Get("repository"), "/")
				require.True(t, found)
				data.Set("doer", owner)
			} else {
				data.SetDefault("repository", "userowner/repositorypublic")
				data.SetDefault("doer", "userowner")
			}
			data.SetDefault("level", "write")
		},
		fixtures: []*fixtureType{
			{
				data: newFixtureData(map[string]string{
					"repository": "userowner/repositorypublic",
					"doer":       "userowner",
					"scope":      scopes,
				}),
			},
			{
				data: newFixtureData(map[string]string{
					"disable-units": units,
				}),
				error: "Not Found",
			},
			{
				data: newFixtureData(map[string]string{
					"doer":       "regularuser",
					"repository": "userowner/repositorypublic",
					"scope":      "write:issue",
				}),
				error: "user should have a permission to write to a repo",
			},
		},
		staticArgs: 1,
		call: func(t *testing.T, ctx apiv1_permissions.Context, _ *fixtureData, args []any) {
			unitType := args[0].([]unit_model.Type)
			t.Logf("calling ReqRepoWriter(ctx, %s)", unitType)
			apiv1_permissions.ReqRepoWriter(ctx, unitType)
		},
	}
})
