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
	"forgejo.org/services/contexttest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChangeFactsRender(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	user := &user_model.User{ID: 1, Name: "priya", LowerName: "priya"}
	ctx, resp := contexttest.MockContext(t, "/user2/repo1/commit/abc")
	ctx.Render = templates.HTMLRenderer()
	contexttest.LoadRepo(t, ctx, 1)
	ctx.Data["RepoLink"] = ctx.Repo.RepoLink
	ctx.Data["ChangeIntent"] = "Because <i>reasons</i>."
	ctx.Data["ChangeFacts"] = &struct{ Additions, Deletions, Files, ChecksPass, ChecksTotal int }{10, 2, 3, 4, 4}
	ctx.Data["ChangeStack"] = &changeStackPos{Name: "retry", Position: 2, Total: 4, PullLink: "/user2/repo1/pulls/2"}
	ctx.Data["ChangeReview"] = &changeReview{Required: 2, Approvals: []changeApproval{{User: user, When: time.Now().Unix()}}}
	ctx.Data["ChangeWhere"] = &changeWhere{OnDefault: true, Branch: "main", Tags: []string{"v2.18.0"}}
	ctx.HTML(http.StatusOK, "repo/change_facts")
	body := resp.Body.String()
	require.Equal(t, http.StatusOK, resp.Code, body)
	assert.Contains(t, body, "Because &lt;i&gt;reasons&lt;/i&gt;.")
	assert.Contains(t, body, "asc_rp.approvals_of")
	assert.Contains(t, body, "asc_rp.in_release")
	assert.Contains(t, body, "asc_rp.stack_pos")
	assert.Contains(t, body, "aria-disabled=\"true\"", "the first and last change have a disabled neighbour")
}
