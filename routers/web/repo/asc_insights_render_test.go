// Copyright 2026 The ASC Hub Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package repo

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"forgejo.org/models/unittest"
	"forgejo.org/modules/templates"
	"forgejo.org/modules/timeutil"
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

// The board notes of Pulse: the who and period controls keep each other, four tiles, a Table with the numbers of the
// bars, the trend chip, recent stacks and the waiting rows that open the change.
func TestInsightsBoardNotes(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	days := []insDay{{People: 1, Agents: 2}, {People: 5}}
	body := renderInsights(t, &insPage{
		Who: "agents", Range: "7d", Days: 7, Landed: 8, Opened: 3, MedianMin: 9, HasMedian: true, AgentPct: 25, HasAgentPct: true,
		TotalPeople: 6, TotalAgent: 2, TotalAll: 8, MaxBar: 5, Days2: days, Bars: insBars(days, 5), Trend: "slower",
		Recent:  []insStack{{Name: "auth-rework", Changes: 3, When: time.Now()}},
		Waiting: []*stackRow{{Name: "cache-split", Link: "/user2/repo1/pulls/2", Updated: timeutil.TimeStampNow()}},
	})

	// the who links keep the period and the period links keep who
	for _, who := range []string{"everyone", "people", "agents"} {
		assert.Contains(t, body, "/user2/repo1/activity?who="+who+"&amp;range=7d")
	}
	for _, rng := range []string{"7d", "30d", "year"} {
		assert.Contains(t, body, "/user2/repo1/activity?who=agents&amp;range="+rng)
	}
	assert.Equal(t, 2, strings.Count(body, `aria-current="true"`), "one choice of each control is current")

	assert.Equal(t, 4, strings.Count(body, `class="ds-card rp-ins-tile"`))
	assert.Contains(t, body, "asc_rp.ins_slower")

	// the Table repeats the numbers the bars draw, day by day
	assert.Contains(t, body, `<td class="ds-r ds-num">1</td><td class="ds-r ds-num">2</td>`)
	assert.Contains(t, body, `<td class="ds-r ds-num">5</td><td class="ds-r ds-num">0</td>`)
	assert.Equal(t, 1, strings.Count(body, `<rect class="rp-bar-agent"`))
	assert.Equal(t, 2, strings.Count(body, `<rect class="rp-bar-people"`))

	assert.Contains(t, body, "auth-rework")
	assert.Contains(t, body, `<a class="ds-mono" href="/user2/repo1/pulls/2">cache-split</a>`)
}
