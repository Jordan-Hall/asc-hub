// Copyright 2026 The Forgejo Authors.
// SPDX-License-Identifier: GPLv3-or-later

package tests_test

import (
	"testing"

	apiv1_permissions "forgejo.org/routers/api/v1/permissions"
)

var _ = registerFunctionTest(apiv1_permissions.MustEnableIssuesOrPulls, functionTest{
	fulfillNeeds: func(t *testing.T, data *fixtureData) {
		t.Helper()
		data.Set("repository-init", "true")
	},
	interpret: func(t *testing.T, permissions *apiv1_permissions.Permissions, data *fixtureData) {
		fixtureDisableUnits(t, permissions, data)
	},
	fixtures: []*fixtureType{
		{
			data: newFixtureData(map[string]string{
				"doer":       "doerregular",
				"repository": "userowner/repositorypublic",
			}),
		},
		{
			data: newFixtureData(map[string]string{
				"doer":            "doerregular",
				"repository":      "userowner/repositorypublic",
				"repository-init": "true",
				"disable-units":   "repo.pulls,repo.issues",
			}),
			error: "Not Found",
		},
	},
})
