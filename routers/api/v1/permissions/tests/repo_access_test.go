// Copyright 2026 The Forgejo Authors.
// SPDX-License-Identifier: GPLv3-or-later

package tests_test

import (
	"testing"

	user_model "forgejo.org/models/user"
	apiv1_permissions "forgejo.org/routers/api/v1/permissions"
)

var _ = registerFunctionTest(apiv1_permissions.RepoAccess, functionTest{
	fulfillNeeds: func(t *testing.T, data *fixtureData) {
		t.Helper()
		data.SetDefault("repository", "userowner/repositorypublic")
	},
	interpret: func(t *testing.T, permissions *apiv1_permissions.Permissions, data *fixtureData) {
		fixtureSetRepository(t, permissions, data)
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
				"doer":       "anonymous",
				"repository": "userowner/repositorypublic",
			}),
		},
		{
			data: newFixtureData(map[string]string{
				"doer":       "doeradmin",
				"repository": "userowner/repositoryprivate",
			}),
		},
		{
			data: newFixtureData(map[string]string{
				"doer":       "doerregular",
				"repository": "userowner/repositoryprivate",
			}),
			error: "Not Found",
		},
		{
			data: newFixtureData(map[string]string{
				"doer":       "anonymous",
				"repository": "userowner/repositoryprivate",
			}),
			error: "Not Found",
		},
		{
			data: newFixtureData(map[string]string{
				"doer":       user_model.ActionsUserName,
				"repository": "userowner/repositorypublic",
			}),
		},
		{
			data: newFixtureData(map[string]string{
				"doer":        user_model.ActionsUserName,
				"repository":  "userowner/repositorypublic",
				"task.RepoID": "unrelated",
			}),
			error: "Not Found",
		},
		{
			data: newFixtureData(map[string]string{
				"doer":                   user_model.ActionsUserName,
				"repository":             "userowner/repositorypublic",
				"task.IsForkPullRequest": "true",
			}),
		},
	},
})
