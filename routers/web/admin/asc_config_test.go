// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package admin

import (
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"forgejo.org/modules/setting"
	"forgejo.org/modules/templates"
	"forgejo.org/modules/test"
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

func TestPlaceConfigFile(t *testing.T) {
	conf := filepath.Join(t.TempDir(), "app.ini")
	defer test.MockVariableValue(&setting.CustomConf, conf)()
	t.Cleanup(setting.RememberConfigFile)
	require.NoError(t, os.WriteFile(conf, []byte("[server]\nDOMAIN = hub.example.org\nSSH_PORT = 2222\n"), 0o600))
	setting.RememberConfigFile()

	ctx, _ := contexttest.MockContext(t, "GET /admin/config")
	where := func(cards []configCard) map[string]configRow {
		rows := map[string]configRow{}
		for _, c := range cards {
			for _, r := range c.Rows {
				rows[r.Key] = r
			}
		}
		return rows
	}
	cards := curatedConfig(ctx)
	st := placeConfigFile(ctx, cards)
	assert.Empty(t, st.Changed)
	assert.Empty(t, st.Problem)
	rows := where(cards)
	assert.Equal(t, ctx.Locale.TrString("asc_admin.config_at_line", "app.ini", 2), rows["server.domain"].Where)
	assert.Equal(t, ctx.Locale.TrString("asc_admin.config_at_line", "app.ini", 3), rows["server.ssh"].Where)
	assert.Equal(t, ctx.Locale.TrString("asc_admin.config_from_default", "app.ini"), rows["mail.from"].Where)
	assert.False(t, rows["server.domain"].Changed)

	require.NoError(t, os.WriteFile(conf, []byte("[server]\nDOMAIN = nova.example.org\nSSH_PORT = 2222\n[mailer]\nFROM = asc@example.org\n"), 0o600))
	cards = curatedConfig(ctx)
	st = placeConfigFile(ctx, cards)
	assert.Equal(t, []string{"server.DOMAIN", "mailer.FROM"}, st.Changed)
	rows = where(cards)
	assert.True(t, rows["server.domain"].Changed, "the running value is not the one in the file")
	assert.False(t, rows["server.ssh"].Changed)
	assert.True(t, rows["mail.from"].Changed)
	assert.Equal(t, ctx.Locale.TrString("asc_admin.config_at_line", "app.ini", 5), rows["mail.from"].Where)

	require.NoError(t, os.Remove(conf))
	st = placeConfigFile(ctx, curatedConfig(ctx))
	assert.Equal(t, ctx.Locale.TrString("asc_admin.config_file_unread", conf), st.Problem)
	assert.Empty(t, st.Changed)
}
