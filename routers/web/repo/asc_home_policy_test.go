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
	"forgejo.org/services/asctrust"
	"forgejo.org/services/contexttest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Board Repo, Policy card: the run budget and the merge queue each get a line, and neither shows when the policy
// does not set it.
func TestAscHomePolicyBudgetAndQueue(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	initSnapshotLocales()
	render := func(sum *asctrust.PolicySummary) string {
		ctx, resp := contexttest.MockContext(t, "/user2/repo1")
		ctx.Render = templates.HTMLRenderer()
		contexttest.LoadRepo(t, ctx, 1)
		ctx.Base.Locale = translation.NewLocale("en-US")
		if ctx.TemplateContext != nil {
			ctx.TemplateContext.Locale = ctx.Base.Locale
		}
		ctx.Data["RepoLink"] = ctx.Repo.RepoLink
		ctx.Data["HomePolicy"] = sum
		ctx.HTML(http.StatusOK, base.TplName("repo/asc_about"))
		body := resp.Body.String()
		require.Equal(t, http.StatusOK, resp.Code, body)
		return body
	}

	body := render(&asctrust.PolicySummary{Present: true, Usable: true, Budget: "20 min and 200k tokens", Queue: true})
	assert.Contains(t, body, `<li data-testid="rp-policy-budget">Agent runs stop at 20 min and 200k tokens.</li>`)
	assert.Contains(t, body, `<li data-testid="rp-policy-queue">Changes land on trunk through the merge queue.</li>`)

	body = render(&asctrust.PolicySummary{Present: true, Usable: true, Lines: []string{"Trunk needs 1 approval."}})
	assert.Contains(t, body, "<li>Trunk needs 1 approval.</li>")
	assert.NotContains(t, body, "rp-policy-budget")
	assert.NotContains(t, body, "rp-policy-queue")
}
