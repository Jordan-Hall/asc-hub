// Copyright 2026 The Forgejo Authors.
// SPDX-License-Identifier: GPLv3-or-later

package tests_test

import (
	apiv1_permissions "forgejo.org/routers/api/v1/permissions"
)

var _ = registerFunctionTest(apiv1_permissions.MustNotBeArchived, functionTest{
	fixtures: []*fixtureType{
		{
			data: newFixtureData(map[string]string{
				"repository": "userowner/repositorypublic",
			}),
		},
		{
			data: newFixtureData(map[string]string{
				"repository": "userowner/repositoryarchived",
			}),
			error: "is archived",
		},
	},
})
