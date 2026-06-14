// Copyright 2026 The Forgejo Authors.
// SPDX-License-Identifier: GPLv3-or-later

package tests_test

import (
	"testing"

	user_model "forgejo.org/models/user"
	apiv1_permissions "forgejo.org/routers/api/v1/permissions"
)

var _ = registerFunctionTest(apiv1_permissions.ReqToken, functionTest{
	fulfillNeeds: func(t *testing.T, data *fixtureData) {
		t.Helper()
		data.SetDefault("doer", "doerregular")
	},
	fixtures: []*fixtureType{
		{
			data: newFixtureData(map[string]string{
				"doer": "doerregular",
			}),
		},
		{
			data: newFixtureData(map[string]string{
				"doer": user_model.ActionsUserName,
			}),
		},
		{
			data: newFixtureData(map[string]string{
				"doer": "anonymous",
			}),
			error: "token is required",
		},
	},
})
