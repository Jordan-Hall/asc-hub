// Copyright 2026 The Forgejo Authors.
// SPDX-License-Identifier: GPLv3-or-later

package tests

import (
	"testing"

	"forgejo.org/modules/setting"
	apiv1_permissions "forgejo.org/routers/api/v1/permissions"
)

var _ = registerFunctionTest(apiv1_permissions.ReqWebhooksEnabled, functionTest{
	protectSettingsBool: []*bool{
		&setting.DisableWebhooks,
	},
	interpret: func(t *testing.T, permissions *apiv1_permissions.Permissions, data *fixtureData) {
		setting.DisableWebhooks = data.Get("DisableWebhooks") == "true"
	},
	fixtures: []*fixtureType{
		{
			data: newFixtureData(map[string]string{}),
		},
		{
			data: newFixtureData(map[string]string{
				"DisableWebhooks": "true",
			}),
			error: "webhooks disabled by administrator",
		},
	},
})
