// Copyright 2026 The ASC Hub Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package repo

import (
	"net/http"
	"testing"

	"forgejo.org/models/unittest"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/templates"
	"forgejo.org/services/contexttest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPersonRowsAndReviewLoad(t *testing.T) {
	rows := personRows([]*ascv1.Contributor{
		{Name: "priya", Kind: ascv1.AuthorKind_AUTHOR_KIND_PERSON, Landed: 40, ReviewsGiven: 12, MedianReviewSeconds: 600, WeeklyLanded: []uint32{1, 3, 2}, Share: 0.466},
		{Name: "coder-2", Kind: ascv1.AuthorKind_AUTHOR_KIND_AGENT, RunBy: "priya", Landed: 46, ReviewsGiven: 5, Share: 0.534},
		{Name: "diego", Kind: ascv1.AuthorKind_AUTHOR_KIND_PERSON, ReviewsGiven: 12},
		{Name: "hannah", Kind: ascv1.AuthorKind_AUTHOR_KIND_PERSON, Share: 7},
	})
	if assert.Len(t, rows, 4) {
		assert.Equal(t, 47, rows[0].SharePct)
		assert.True(t, rows[0].HasMedian)
		assert.Equal(t, 10, rows[0].MedianMin)
		assert.NotEmpty(t, rows[0].Spark)
		assert.True(t, rows[1].Agent)
		assert.Equal(t, "priya", rows[1].RunBy)
		assert.Equal(t, 100, rows[3].SharePct, "a share outside 0 to 1 cannot overflow the bar")
	}
	load := reviewLoadOf(rows)
	if assert.Len(t, load, 2, "an agent never appears in the review load, nor does a person with no review") {
		assert.Equal(t, "diego", load[0].Name, "ties are ordered by name")
		assert.Equal(t, "priya", load[1].Name)
	}
}

func TestPeopleRender(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	ctx, resp := contexttest.MockContext(t, "/user2/repo1/activity/contributors")
	ctx.Render = templates.HTMLRenderer()
	contexttest.LoadRepo(t, ctx, 1)
	ctx.Data["RepoLink"] = ctx.Repo.RepoLink
	ctx.Data["AscPeople"] = &insPeoplePage{
		Range: "30d", HasAgentPct: true, AgentPct: 54, PeoplePct: 46, AllApproved: true,
		Rows: []insPerson{
			{Name: "priya <b>", Landed: 3, Reviews: 2, Spark: "0.0,23.0 100.0,1.0", SharePct: 40},
			{Name: "coder-2", Agent: true, RunBy: "priya", Landed: 4, SharePct: 60},
		},
		ReviewLoad: []insPerson{{Name: "priya <b>", Reviews: 2}},
	}
	ctx.HTML(http.StatusOK, "repo/insights_people")
	body := resp.Body.String()
	require.Equal(t, http.StatusOK, resp.Code, body)
	assert.Contains(t, body, "priya &lt;b&gt;")
	assert.NotContains(t, body, "priya <b>")
	assert.Contains(t, body, "asc_rp.run_by")
	assert.Contains(t, body, "<polyline")
	assert.Contains(t, body, "asc_rp.agents_never_review")
}
