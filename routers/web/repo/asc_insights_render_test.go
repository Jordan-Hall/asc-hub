// Copyright 2026 The ASC Hub Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package repo

import (
	"net/http"
	"testing"
	"time"

	"forgejo.org/models/unittest"
	"forgejo.org/modules/templates"
	"forgejo.org/services/contexttest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInsightsRender(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	ctx, resp := contexttest.MockContext(t, "/user2/repo1/activity")
	ctx.Render = templates.HTMLRenderer()
	contexttest.LoadRepo(t, ctx, 1)
	ctx.Data["RepoLink"] = ctx.Repo.RepoLink
	ctx.Data["AscInsightsLink"] = ctx.Repo.RepoLink + "/activity"
	days := []insDay{{People: 3}, {People: 1, Agents: 2}}
	ctx.Data["AscInsights"] = &insPage{
		Who: "everyone", Range: "30d", Days: 30, Landed: 4, HasLandedDelta: true, LandedDelta: 12, Opened: 2, MedianMin: 18, HasMedian: true,
		AgentPct: 50, HasAgentPct: true, AllAgentsApproved: true, TotalPeople: 4, TotalAgent: 2, TotalAll: 6, MaxBar: 3,
		Days2: days, Bars: insBars(days, 3), Weeks: []insWeek{{Start: time.Now(), Minutes: 12, N: 2}}, Trend: "faster",
		Sizes: []insSizeGroup{{Key: "under_50", Minutes: 6, N: 3}}, Recent: []insStack{{Name: "docs", Changes: 1, When: time.Now()}},
	}
	ctx.HTML(http.StatusOK, "repo/insights_asc")
	body := resp.Body.String()
	require.Equal(t, http.StatusOK, resp.Code, body)
	assert.Contains(t, body, `<rect class="rp-bar-people"`)
	assert.Contains(t, body, `<rect class="rp-bar-agent"`)
	assert.Contains(t, body, "asc_rp.ins_size_under_50")
	assert.Contains(t, body, `aria-current="true"`)
}

func renderInsights(t *testing.T, pg *insPage) string {
	t.Helper()
	ctx, resp := contexttest.MockContext(t, "/user2/repo1/activity")
	ctx.Render = templates.HTMLRenderer()
	contexttest.LoadRepo(t, ctx, 1)
	ctx.Data["RepoLink"] = ctx.Repo.RepoLink
	ctx.Data["AscInsightsLink"] = ctx.Repo.RepoLink + "/activity"
	ctx.Data["AscInsights"] = pg
	ctx.HTML(http.StatusOK, "repo/insights_asc")
	require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
	return resp.Body.String()
}

func TestInsightsRenderStates(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())

	body := renderInsights(t, &insPage{Who: "everyone", Range: "30d", Days: 30, New: true})
	assert.Contains(t, body, `data-testid="asc-insights-new"`)
	assert.Contains(t, body, "asc_rp.ins_new_body")
	assert.NotContains(t, body, "rp-ins-tiles", "a new repository shows no tiles of zeros")
	assert.NotContains(t, body, "data-ins-segs")

	body = renderInsights(t, &insPage{Who: "everyone", Range: "30d", Days: 30, Behind: true})
	assert.Contains(t, body, `data-testid="asc-insights-behind"`)
	assert.Contains(t, body, "asc_rp.ins_behind")
	assert.NotContains(t, body, `data-testid="asc-insights-new"`)

	body = renderInsights(t, &insPage{Who: "everyone", Range: "30d", Days: 30})
	assert.NotContains(t, body, `data-testid="asc-insights-behind"`)
	assert.Contains(t, body, "<template data-ins-skeleton>", "the loading state is drawn from the page, not from script text")
	assert.Contains(t, body, `data-loading="asc_rp.ins_loading"`)
	assert.Contains(t, body, "data-ins-body")
}
