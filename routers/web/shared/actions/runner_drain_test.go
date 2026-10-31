// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package actions

import (
	"net/http"
	"net/url"
	"testing"

	actions_model "forgejo.org/models/actions"
	"forgejo.org/models/unittest"
	"forgejo.org/services/contexttest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunnerDrainPostSetsAndClearsTheSwitchForAnAdministrator(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	drain := func(value string) (*actions_model.ActionRunner, int) {
		ctx, resp := contexttest.MockContext(t, "POST admin/actions/runners/1/drain")
		contexttest.LoadUser(t, ctx, 1)
		ctx.IsSigned = true
		ctx.Data["PageIsAdmin"] = true
		ctx.SetParams("runnerid", "10000001")
		ctx.Req.Form = url.Values{"drain": {value}}
		RunnerDrainPost(ctx)
		r, err := actions_model.GetRunnerByID(t.Context(), 10000001)
		require.NoError(t, err)
		return r, resp.Code
	}
	r, code := drain("1")
	assert.Equal(t, http.StatusSeeOther, code)
	assert.True(t, r.Draining)
	r, _ = drain("1")
	assert.True(t, r.Draining, "asking twice changes nothing")
	r, _ = drain("0")
	assert.False(t, r.Draining)
	r, _ = drain("garbage")
	assert.False(t, r.Draining, "anything but 1 is off")
}

func TestRunnerDrainPostRefusesARunnerOfAnotherOwner(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	// runner 10000004 belongs to user 1; the user settings page of user 2 may only change that user's own runners
	ctx, _ := contexttest.MockContext(t, "POST user/settings/actions/runners/1/drain")
	contexttest.LoadUser(t, ctx, 2)
	ctx.IsSigned = true
	ctx.Data["PageIsUserSettings"] = true
	ctx.SetParams("runnerid", "10000004")
	ctx.Req.Form = url.Values{"drain": {"1"}}
	RunnerDrainPost(ctx)
	r, err := actions_model.GetRunnerByID(t.Context(), 10000004)
	require.NoError(t, err)
	assert.False(t, r.Draining)
}
