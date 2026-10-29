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
