// Copyright 2026 The ASC Hub Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package repo

import (
	"net/http"
	"testing"
	"time"

	issues_model "forgejo.org/models/issues"
	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/base"
	"forgejo.org/modules/templates"
	"forgejo.org/services/contexttest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChangeFactsRender(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	user := &user_model.User{ID: 1, Name: "priya", LowerName: "priya"}
	render := func(tpl string) string {
		ctx, resp := contexttest.MockContext(t, "/user2/repo1/commit/abc")
		ctx.Render = templates.HTMLRenderer()
		contexttest.LoadRepo(t, ctx, 1)
		ctx.Data["RepoLink"] = ctx.Repo.RepoLink
		ctx.Data["ChangeIntent"] = "Because <i>reasons</i>."
		ctx.Data["ChangeFacts"] = &struct{ Additions, Deletions, Files, ChecksPass, ChecksTotal int }{10, 2, 3, 4, 4}
		ctx.Data["ChangeStack"] = &changeStackPos{Name: "retry", Position: 2, Total: 4, PullLink: "/user2/repo1/pulls/2"}
		ctx.Data["ChangeReview"] = &changeReview{Required: 2, Approvals: []changeApproval{{User: user, When: time.Now().Unix()}}}
		ctx.Data["ChangeWhere"] = &changeWhere{OnDefault: true, Branch: "main", Tags: []string{"v2.18.0"}}
		ctx.Data["ChangeChecks"] = []*changeCheck{{Name: "build", State: "success", Tone: "ok"}, {Name: "lint", State: "failure", Tone: "err", Target: "https://ci.example.org/lint"}}
		ctx.Data["ChangeRevertLink"] = "/user2/repo1/pulls/2/asc/revert"
		ctx.Data["AscChangeFiles"] = ascChangeFilesOf(diffOf("src/a.go", "go.sum"), map[string]ascv1.ChangeClass{"go.sum": ascv1.ChangeClass_CHANGE_CLASS_LOCKFILE}, false, "/user2/repo1/commit/abc")
		ctx.HTML(http.StatusOK, base.TplName(tpl))
		body := resp.Body.String()
		require.Equal(t, http.StatusOK, resp.Code, body)
		return body
	}

	// the left column: intent, facts strip and stack position
	body := render("repo/change_facts")
	assert.Contains(t, body, "Because &lt;i&gt;reasons&lt;/i&gt;.")
	assert.Contains(t, body, "asc_rp.stack_pos")
	assert.Contains(t, body, "aria-disabled=\"true\"", "the first and last change have a disabled neighbour")
	assert.Contains(t, body, `data-testid="change-groups"`)
	assert.Contains(t, body, `data-group="generated"`)
	assert.NotContains(t, body, `data-group="generated" open`, "generated files start folded")
	assert.Contains(t, body, `data-group="behaviour" open`)
	assert.NotContains(t, body, "change-large")

	// the right rail: review, checks, where it is now and undo
	rail := render("repo/change_rail")
	assert.Contains(t, rail, "asc_rp.approvals_of")
	assert.Contains(t, rail, "asc_rp.in_release")
	assert.Contains(t, rail, "build")
	assert.Contains(t, rail, "https://ci.example.org/lint")
	assert.Contains(t, rail, "/user2/repo1/pulls/2/asc/revert")
}

func TestResolvedConversationStartsCollapsedUnlessTheSwitchIsOff(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	render := func(expand bool) string {
		ctx, resp := contexttest.MockContext(t, "/user2/repo1/pulls/2")
		ctx.Render = templates.HTMLRenderer()
		contexttest.LoadRepo(t, ctx, 1)
		c := &issues_model.Comment{ID: 7, Type: issues_model.CommentTypeCode, Line: 3, TreePath: "a.go", Invalidated: false, ResolveDoerID: 2, Review: &issues_model.Review{Type: issues_model.ReviewTypeComment}}
		ctx.Data["comments"] = []*issues_model.Comment{c}
		ctx.Data["AscExpandResolved"] = expand
		ctx.Data["Editorconfig"] = nil
		ctx.HTML(http.StatusOK, "repo/issue/view_content/conversation")
		return resp.Body.String()
	}
	collapsed := render(false)
	require.NotContains(t, collapsed, "panic")
	assert.Contains(t, collapsed, `id="code-comments-7" class="comment-code-cloud ds-card-b tw-hidden"`, "resolved: collapsed")
	expanded := render(true)
	assert.Contains(t, expanded, `id="code-comments-7" class="comment-code-cloud ds-card-b"`, "switch off: open")
}
