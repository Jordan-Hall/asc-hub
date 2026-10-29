// Copyright 2026 The ASC Hub Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package repo

import (
	"net/http"
	"testing"
	"time"

	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/templates"
	"forgejo.org/modules/timeutil"
	"forgejo.org/services/contexttest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testRows() []*stackRow {
	person := &user_model.User{ID: 1, Name: "priya", LowerName: "priya"}
	bot := &user_model.User{ID: 2, Name: "coder-2", LowerName: "coder-2", Type: user_model.UserTypeBot}
	return []*stackRow{
		{
			Name: "retry-with-backoff", Title: "Retry every call <b>with</b> backoff", Link: "/user2/repo1/pulls/3", Status: stackNeedsReview, Tone: "warn",
			People: []*user_model.User{person, bot}, Agent: true, Mine: true, NeedsMe: true, Changes: 4, Landed: 2, Open: 2,
			Segments:  []stackSegment{{State: "landed"}, {State: "landed"}, {State: "warn", Link: "/user2/repo1/pulls/3", Title: "a"}, {State: "agent", Link: "/user2/repo1/pulls/4", Title: "b"}},
			Additions: 312, Deletions: 48, HasSize: true, ChecksPass: 12, ChecksTotal: 12, Updated: timeutil.TimeStamp(time.Now().Unix()),
		},
		{
			Name: "ledger-export-csv", Title: "Export", Link: "/user2/repo1/pulls/9", Status: stackFailing, Tone: "err",
			People: []*user_model.User{person}, Changes: 1, Open: 1, Segments: []stackSegment{{State: "err", Link: "/user2/repo1/pulls/9"}},
			ChecksFail: 1, ChecksTotal: 31, Updated: timeutil.TimeStamp(time.Now().Unix()),
		},
	}
}

func TestStackBoardRenders(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	ctx, resp := contexttest.MockContext(t, "/user2/repo1/pulls")
	ctx.Render = templates.HTMLRenderer()
	contexttest.LoadRepo(t, ctx, 1)
	rows := testRows()
	ctx.Data["RepoLink"] = ctx.Repo.RepoLink
	ctx.Data["Repository"] = ctx.Repo.Repository
	ctx.Data["IsSigned"] = true
	ctx.Data["StackRows"] = rows
	ctx.Data["StackCounts"] = stackFilterCounts(rows)
	ctx.Data["StackSort"] = stackSortAttention
	ctx.Data["StackYourTurn"] = stackYourTurn(rows)
	ctx.HTML(http.StatusOK, "repo/stack_board")
	body := resp.Body.String()
	require.Equal(t, http.StatusOK, resp.Code, body)

	assert.Contains(t, body, `data-stack-show="needsme"`)
	assert.Contains(t, body, `data-stack-row data-mine="true" data-agent="true" data-needsme="true"`)
	assert.Contains(t, body, "retry-with-backoff")
	assert.Contains(t, body, "asc_rp.st_needs_review")
	assert.Contains(t, body, "asc_rp.st_failing")
	assert.Contains(t, body, "asc_rp.your_turn", "the first row that waits for the viewer gets the card")
	assert.Contains(t, body, `href="/user2/repo1/pulls/3"`)
	assert.Contains(t, body, "+312")
	assert.Contains(t, body, "asc_rp.checks_n_failing")
	assert.Contains(t, body, "rp-seg-landed")
	assert.Contains(t, body, "Retry every call &lt;b&gt;with&lt;/b&gt; backoff", "a title is text, never markup")
	assert.NotContains(t, body, "<b>with</b>")
}

func TestStackBoardEmptyAndFailed(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	render := func(set func(ctxData map[string]any)) string {
		ctx, resp := contexttest.MockContext(t, "/user2/repo1/pulls")
		ctx.Render = templates.HTMLRenderer()
		contexttest.LoadRepo(t, ctx, 1)
		ctx.Data["RepoLink"] = ctx.Repo.RepoLink
		ctx.Data["Repository"] = ctx.Repo.Repository
		set(ctx.Data)
		ctx.HTML(http.StatusOK, "repo/stack_board")
		require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
		return resp.Body.String()
	}
	empty := render(func(d map[string]any) { d["StackCounts"] = stackCounts{}; d["StackSort"] = stackSortAttention })
	assert.Contains(t, empty, `data-testid="asc-stacks-empty"`)
	failed := render(func(d map[string]any) { d["StackListFailed"] = true; d["StackRequestID"] = "req-1" })
	assert.Contains(t, failed, `role="alert"`)
	assert.Contains(t, failed, "asc_rp.stacks_failed_id")
	assert.NotContains(t, failed, "data-stack-row")
}
