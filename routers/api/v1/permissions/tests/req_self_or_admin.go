// Copyright 2026 The Forgejo Authors.
// SPDX-License-Identifier: GPLv3-or-later

package tests

import (
	"testing"

	user_model "forgejo.org/models/user"
	apiv1_permissions "forgejo.org/routers/api/v1/permissions"
)

var _ = registerFunctionTest(apiv1_permissions.ReqSelfOrAdmin, functionTest{
	fulfillNeeds: func(t *testing.T, data *fixtureData) {
		t.Helper()
		data.SetDefault("doer", "doeradmin")
	},
	interpret: func(t *testing.T, permissions *apiv1_permissions.Permissions, data *fixtureData) {
		if data.Has("user") && data.Get("user") != "anonymous" {
			name := data.Get("user")
			user := permissions.User()
			if user == nil {
				fixtureCreateUser(t, &user_model.User{Name: name})
				permissions.SetUser(fixtureGetUser(t, name))
			}
		}
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
				"user": "regularuser",
			}),
		},
		{
			data: newFixtureData(map[string]string{
				"doer": "regularuser",
				"user": "otheruser",
			}),
			error: "doer should be the site admin or be same as the contextUser",
		},
	},
})
