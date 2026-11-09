// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	"net/http"
	"strings"
	"testing"

	issues_model "forgejo.org/models/issues"
	"forgejo.org/models/unittest"
	"forgejo.org/modules/base"
	"forgejo.org/modules/templates"
	"forgejo.org/services/contexttest"
	pull_service "forgejo.org/services/pull"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A row that no stack and no quarantined group names has no chip, even when another row of the page has one: the chips
// are maps of values, and a missing key gives the zero value, which a template "with" takes as true.
func TestOnlyTheRowsWithAChipShowIt(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	ctx, resp := contexttest.MockContext(t, "/user2/repo1/issues")
	ctx.Render = templates.HTMLRenderer()
	contexttest.LoadRepo(t, ctx, 1)
	list := issues_model.IssueList{
		unittest.AssertExistsAndLoadBean(t, &issues_model.Issue{ID: 1}),
		unittest.AssertExistsAndLoadBean(t, &issues_model.Issue{ID: 5}),
	}
	require.NoError(t, list.LoadAttributes(ctx))
	ctx.Data["Issues"] = list
	ctx.Data["AscIssueChips"] = map[int64]pull_service.AscIssueStack{1: {
		Entries: []pull_service.AscStackEntry{{Index: 2, State: pull_service.AscStackStateMerged}},
		Landed:  1,
		Top:     pull_service.AscStackEntry{Index: 2, Link: "/user2/repo1/pulls/2"},
	}}
	ctx.Data["AscIssueQuarantine"] = map[int64]ascIssueQuarantineChip{1: {Test: "t", Link: "/user2/repo1/asc/checks/groups/4"}}
	ctx.HTML(http.StatusOK, base.TplName("shared/issuelist"))
	body := resp.Body.String()
	require.Equal(t, http.StatusOK, resp.Code, body)
	assert.Equal(t, 1, strings.Count(body, `data-testid="issue-stack-chip"`))
	assert.Equal(t, 1, strings.Count(body, `data-testid="issue-quarantine-chip"`))
	assert.Contains(t, body, `class="ds-chip ds-chip-ok" href="/user2/repo1/pulls/2"`, "a stack that has landed whole")
}
