// Copyright 2026 The Forgejo Authors.
// SPDX-License-Identifier: GPLv3-or-later

package tests_test

import (
	"testing"

	user_model "forgejo.org/models/user"
	apiv1_permissions "forgejo.org/routers/api/v1/permissions"
)

var _ = registerFunctionTest(apiv1_permissions.IndividualPermsChecker, functionTest{
	fulfillNeeds: func(t *testing.T, data *fixtureData) {
		t.Helper()
		data.SetDefault("user", data.Get("doer"))
	},
	interpret: func(t *testing.T, permissions *apiv1_permissions.Permissions, data *fixtureData) {
		if data.Has("user") && data.Get("user") != "anonymous" {
			name := data.Get("user")
			fixtureCreateUser(t, &user_model.User{Name: name})
			permissions.SetUser(fixtureGetUser(t, name))
		}
	},
	fixtures: []*fixtureType{
		{
			data: newFixtureData(map[string]string{
				"user": "IndividualPermsChecker",
			}),
		},
		{
			data: newFixtureData(map[string]string{
				"user": "IndividualPermsCheckerprivate",
			}),
			error: "Visit Project",
		},
		{
			data: newFixtureData(map[string]string{
				"doer": "anonymous",
				"user": "IndividualPermsCheckerlimited",
			}),
			error: "Visit Project",
		},
	},
})
