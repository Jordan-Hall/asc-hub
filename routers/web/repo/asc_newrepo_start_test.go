// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	"testing"

	"forgejo.org/models/unittest"
	"forgejo.org/modules/setting"
	api "forgejo.org/modules/structs"
	"forgejo.org/modules/test"
	"forgejo.org/services/contexttest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewRepoStartFrom(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	create := func() map[string]any {
		ctx, _ := contexttest.MockContext(t, "repo/create")
		contexttest.LoadUser(t, ctx, 2)
		Create(ctx)
		return ctx.Data
	}
	imports, _ := create()["NewRepoImports"].([]api.GitServiceType)
	require.NotEmpty(t, imports)
	assert.Equal(t, api.PlainGitService, imports[0], "plain git comes first")
	t.Run("migrations off", func(t *testing.T) {
		defer test.MockVariableValue(&setting.Repository.DisableMigrations, true)()
		assert.Nil(t, create()["NewRepoImports"], "no importers: no Import")
	})

	empty := renderPart(t, "repo/create_start", func(d map[string]any) { d["NewRepoImports"] = imports })
	assert.Contains(t, empty, `data-start="empty" aria-pressed="true"`)
	assert.Contains(t, empty, `data-start="template" aria-pressed="false"`)
	assert.Contains(t, empty, `data-start="import" aria-pressed="false"`)
	tpl := renderPart(t, "repo/create_start", func(d map[string]any) { d["repo_template"] = int64(12) })
	assert.Contains(t, tpl, `data-start="template" aria-pressed="true"`, "a template given in the link starts on Template")
	assert.NotContains(t, tpl, `data-start="import"`)

	list := renderPart(t, "repo/create_import", func(d map[string]any) { d["NewRepoImports"] = imports })
	assert.Contains(t, list, `data-testid="newrepo-import"`)
	assert.Contains(t, list, `href="/repo/migrate?service_type=1" data-import-link`)
	assert.Contains(t, list, "migrate.github.description")
	assert.NotContains(t, renderPart(t, "repo/create_import", func(map[string]any) {}), "newrepo-import")
}

func TestMigrateTakesWhatWasTyped(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	migrate := func(query string) map[string]any {
		ctx, _ := contexttest.MockContext(t, "repo/migrate?service_type=2&org=2&"+query)
		contexttest.LoadUser(t, ctx, 2)
		Migrate(ctx)
		return ctx.Data
	}
	d := migrate("repo_name=ledger&description=Books&private=1")
	assert.Equal(t, "ledger", d["repo_name"])
	assert.Equal(t, "Books", d["description"])
	assert.Equal(t, true, d["private"])
	assert.Equal(t, false, migrate("private=0")["private"])

	defer test.MockVariableValue(&setting.Repository.ForcePrivate, true)()
	assert.Equal(t, migrate("")["private"], migrate("private=0")["private"], "a server that forces private ignores the link")
}
