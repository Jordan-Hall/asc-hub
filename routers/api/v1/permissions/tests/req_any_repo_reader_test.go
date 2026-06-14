// Copyright 2026 The Forgejo Authors.
// SPDX-License-Identifier: GPLv3-or-later

package tests_test

import (
	apiv1_permissions "forgejo.org/routers/api/v1/permissions"
)

var _ = registerFunctionTest(apiv1_permissions.ReqAnyRepoReader, functionTest{
	sequenceFilter: []string{
		"APIAuthorization",
		"RepoAccess",
		"ReqAnyRepoReader",
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
				"doer":       "doeradmin",
				"repository": "userowner/repositoryprivate",
			}),
		},
		// This fixture is unreachable because this permissions function is always used after
		// a RepoAccess that enforces the same restriction for non admin users
		// {
		// 	data: newFixtureData(map[string]string{
		// 		"doer":       "doerregular",
		// 		"repository": "userowner/repositoryprivate",
		// 	}),
		// 	error: "Denied",
		// },
	},
},
)
