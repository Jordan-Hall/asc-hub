// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	"net/http"
	"testing"

	"forgejo.org/models/unittest"
	"forgejo.org/modules/base"
	"forgejo.org/modules/templates"
	"forgejo.org/modules/translation"
	"forgejo.org/services/contexttest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Board Refs, stack clean-up note: the Stacks section says how many days a stack stays after its last change lands
// when the policy sets `[stacks] cleanup_days`, and says nothing when it leaves the days to the server.
func TestAscRefsStackCleanupNote(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	initSnapshotLocales()
	render := func(days any) string {
		ctx, resp := contexttest.MockContext(t, "/user2/repo1/branches")
		ctx.Render = templates.HTMLRenderer()
		contexttest.LoadUser(t, ctx, 2)
		contexttest.LoadRepo(t, ctx, 1)
		ctx.Base.Locale = translation.NewLocale("en-US")
		if ctx.TemplateContext != nil {
			ctx.TemplateContext.Locale = ctx.Base.Locale
		}
		ctx.Data["RepoLink"] = ctx.Repo.RepoLink
		ctx.Data["RefStacks"] = []*stackRow{{Name: "login", Title: "Sign in with a passkey", Link: "/user2/repo1/pulls/1", Changes: 2, Open: 2}}
		ctx.Data["RefStackTotal"] = 1
		if days != nil {
			ctx.Data["RefStackCleanupDays"] = days
		}
		ctx.HTML(http.StatusOK, base.TplName("repo/branch/list"))
		body := resp.Body.String()
		require.Equal(t, http.StatusOK, resp.Code, body)
		return body
	}

	assert.Contains(t, render(int64(7)), `<p class="rp-sec-sub" data-testid="ref-stacks-cleanup">Stacks clean themselves up 7 days after the last change lands.</p>`)
	assert.Contains(t, render(int64(1)), `Stacks clean themselves up 1 day after the last change lands.`)
	assert.NotContains(t, render(nil), "ref-stacks-cleanup")
}
