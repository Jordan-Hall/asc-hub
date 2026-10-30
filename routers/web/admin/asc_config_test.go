// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package admin

import (
	"net/http"
	"net/url"
	"testing"

	"forgejo.org/modules/setting"
	"forgejo.org/modules/templates"
	"forgejo.org/services/asctrust"
	"forgejo.org/services/contexttest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChangeConfigAscSettings(t *testing.T) {
	f := startInstanceAgents(t, asctrust.FeatureOrgSettings)

	change := func(key, value string, user int64) (int, string) {
		ctx, rec := contexttest.MockContext(t, "POST /admin/config")
		ctx.Render = templates.HTMLRenderer()
		contexttest.LoadUser(t, ctx, user)
		ctx.IsSigned = true
		form := url.Values{"key": {key}, "value": {value}}
		ctx.Req.Form, ctx.Req.PostForm = form, form
		ChangeConfig(ctx)
		return rec.Code, rec.Body.String()
	}

	code, body := change("asc.allow_agents", "false", 1)
	assert.Equal(t, http.StatusOK, code, body)
	require.Len(t, f.sets, 1)
	assert.False(t, f.sets[0].GetAllowAgents())
	assert.EqualValues(t, 400_000_000, f.sets[0].GetDefaultBudgetMicroUsd(), "the budget the switch did not name keeps its value")

	code, body = change("asc.default_budget", "12.5", 1)
	assert.Equal(t, http.StatusOK, code, body)
	require.Len(t, f.sets, 2)
	assert.True(t, f.sets[1].GetAllowAgents(), "the switch the budget did not name keeps its value")
	assert.EqualValues(t, 12_500_000, f.sets[1].GetDefaultBudgetMicroUsd())

	code, _ = change("asc.default_budget", "-5", 1)
	assert.NotEqual(t, http.StatusOK, code, "a bad amount is refused")
	code, _ = change("asc.something_else", "1", 1)
	assert.NotEqual(t, http.StatusOK, code, "a key that is not a setting is refused")
	assert.Len(t, f.sets, 2)
}

func TestLoadAscRuntime(t *testing.T) {
	startInstanceAgents(t, asctrust.FeatureOrgSettings)
	ctx, _ := contexttest.MockContext(t, "GET /admin/config")
	contexttest.LoadUser(t, ctx, 1)
	r := loadAscRuntime(ctx)
	assert.True(t, r.Available)
	assert.Empty(t, r.Problem)
	assert.True(t, r.Settings.AllowAgents)
	assert.EqualValues(t, 400_000_000, r.Settings.BudgetMicro)
}

func TestLoadAscRuntimeWithoutTheService(t *testing.T) {
	startInstanceAgents(t) // asc serve without the organisation settings: the rows are left out
	ctx, _ := contexttest.MockContext(t, "GET /admin/config")
	contexttest.LoadUser(t, ctx, 1)
	assert.False(t, loadAscRuntime(ctx).Available)
}

func TestCuratedConfigReadsTheRunningConfiguration(t *testing.T) {
	ctx, _ := contexttest.MockContext(t, "GET /admin/config")
	cards := curatedConfig(ctx)
	require.Len(t, cards, 3)
	keys := map[string]string{}
	for _, c := range cards {
		assert.NotEmpty(t, c.Title)
		assert.NotEmpty(t, c.Note)
		for _, r := range c.Rows {
			keys[r.Key] = r.Value
			assert.NotEmpty(t, r.Label, r.Key)
			assert.NotEmpty(t, r.Value, r.Key)
		}
	}
	assert.Equal(t, setting.Domain, keys["server.domain"])
	assert.Equal(t, string(setting.Protocol), keys["server.protocol"])
	assert.Contains(t, keys, "mail.from")
	assert.Contains(t, keys, "storage.byo")
}
