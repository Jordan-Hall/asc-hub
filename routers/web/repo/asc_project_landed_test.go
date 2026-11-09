// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	"net/http"
	"strings"
	"testing"

	"forgejo.org/models/db"
	issues_model "forgejo.org/models/issues"
	project_model "forgejo.org/models/project"
	"forgejo.org/models/unittest"
	"forgejo.org/modules/templates"
	"forgejo.org/services/contexttest"
	pull_service "forgejo.org/services/pull"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTheColumnMenuChoosesTheLandedColumn(t *testing.T) {
	unittest.PrepareTestEnv(t)
	set := func(column, landed string) int {
		ctx, resp := contexttest.MockContext(t, "POST /user2/repo1/projects/1/"+column+"/landed?landed="+landed)
		contexttest.LoadUser(t, ctx, 2)
		contexttest.LoadRepo(t, ctx, 1)
		ctx.SetParams(":id", "1")
		ctx.SetParams(":columnID", column)
		SetProjectColumnLanded(ctx)
		return resp.Code
	}
	require.Equal(t, http.StatusOK, set("3", "true"))
	assert.True(t, unittest.AssertExistsAndLoadBean(t, &project_model.Column{ID: 3}).AscLanded)
	require.Equal(t, http.StatusOK, set("2", "true"))
	assert.False(t, unittest.AssertExistsAndLoadBean(t, &project_model.Column{ID: 3}).AscLanded, "one Landed column per project")
	assert.True(t, unittest.AssertExistsAndLoadBean(t, &project_model.Column{ID: 2}).AscLanded)
	require.Equal(t, http.StatusOK, set("2", "false"))
	assert.False(t, unittest.AssertExistsAndLoadBean(t, &project_model.Column{ID: 2}).AscLanded)

	ctx, _ := contexttest.MockContext(t, "POST /user2/repo1/projects/1/2/landed?landed=true")
	contexttest.LoadUser(t, ctx, 4)
	contexttest.LoadRepo(t, ctx, 1)
	ctx.SetParams(":id", "1")
	ctx.SetParams(":columnID", "2")
	SetProjectColumnLanded(ctx)
	assert.False(t, unittest.AssertExistsAndLoadBean(t, &project_model.Column{ID: 2}).AscLanded, "a reader cannot choose it")
}

func TestTheBoardShowsTheStackOfACardAndTheLandedColumn(t *testing.T) {
	unittest.PrepareTestEnv(t)
	pr1 := unittest.AssertExistsAndLoadBean(t, &issues_model.PullRequest{ID: 1})
	pr2 := unittest.AssertExistsAndLoadBean(t, &issues_model.PullRequest{ID: 2})
	require.NoError(t, issues_model.SetPullRequestAscStack(db.DefaultContext, pr1, "kkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkk", 0))
	require.NoError(t, issues_model.SetPullRequestAscStack(db.DefaultContext, pr2, "llllllllllllllllllllllllllllllll", pr1.ID))
	require.NoError(t, project_model.SetLandedColumn(db.DefaultContext, 1, 3, true))

	ctx, resp := contexttest.MockContext(t, "user2/repo1/projects/1")
	ctx.Render = templates.HTMLRenderer()
	contexttest.LoadUser(t, ctx, 2)
	contexttest.LoadRepo(t, ctx, 1)
	ctx.ContextUser = ctx.Doer
	ctx.Data["Link"] = "/user2/repo1/projects/1"
	ctx.SetParams(":id", "1")
	ViewProject(ctx)
	body := resp.Body.String()
	require.Equal(t, http.StatusOK, resp.Code, body)

	stacks, ok := ctx.Data["AscCardStacks"].(map[int64]pull_service.AscIssueStack)
	require.True(t, ok)
	require.Contains(t, stacks, int64(3), "the card of pull 2 follows its stack")
	assert.Equal(t, 1, stacks[3].Landed)
	assert.Equal(t, len(stacks), strings.Count(body, `data-testid="project-card-stack"`), "a card that no stack names has no chip")
	assert.Contains(t, body, "asc_work.pj_card_stack")
	assert.Contains(t, body, "/user2/repo1/pulls/2#asc-stack-title")
	assert.Contains(t, body, `data-testid="project-column-landed"`)
	assert.Contains(t, body, `data-url="/user2/repo1/projects/1/3/landed" data-landed="false"`)
	assert.Contains(t, body, `data-url="/user2/repo1/projects/1/2/landed" data-landed="true"`)
}
