// Copyright 2026 The Forgejo Authors.
// SPDX-License-Identifier: GPLv3-or-later

package tests_test

import (
	"testing"

	user_model "forgejo.org/models/user"
	apiv1_permissions "forgejo.org/routers/api/v1/permissions"
)

var _ = registerFunctionTest(apiv1_permissions.APIAuthorization, functionTest{
	fulfillNeeds: func(t *testing.T, data *fixtureData) {
		t.Helper()
		data.SetDefault("doer", "doerregular")
		if data.Get("doer") == user_model.ActionsUserName {
			data.SetDefault("repository", "userowner/repositorypublic")
		}
		data.SetDefault("scope", "read:repository")
		data.SetDefault("level", "read")
	},
	interpret: func(t *testing.T, permissions *apiv1_permissions.Permissions, data *fixtureData) {
		if data.Has("repository") && data.Get("doer") == user_model.ActionsUserName {
			fixtureSetRepository(t, permissions, data)
		}
		fixtureSetDoer(t, permissions, data)
	},
	fixtures: []*fixtureType{
		{
			data: newFixtureData(map[string]string{
				"doer": "anonymous",
			}),
		},
		{
			data: newFixtureData(map[string]string{
				"doer": "doerregular",
			}),
		},
	},
})
