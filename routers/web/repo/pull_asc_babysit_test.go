// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	"net/http"
	"testing"

	issues_model "forgejo.org/models/issues"
	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/test"
	"forgejo.org/services/ascbabysit"
	"forgejo.org/services/context"
	"forgejo.org/services/contexttest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// babysitCtx is a request for pull request 2 (#3 of user2/repo1) by user, after user 4 asked coder-2 to watch it.
func babysitCtx(t *testing.T, user int64) (*context.Context, *issues_model.PullRequest) {
	t.Helper()
	require.NoError(t, unittest.PrepareTestDatabase())
	t.Cleanup(test.MockVariableValue(&setting.Asc.Enabled, true))
	pr := unittest.AssertExistsAndLoadBean(t, &issues_model.PullRequest{ID: 2})
	require.NoError(t, pr.LoadIssue(t.Context()))
	user4 := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 4})
	require.NoError(t, ascbabysit.Watch(t.Context(), user4, pr, "coder-2"))
	ctx, _ := contexttest.MockContext(t, "POST /user2/repo1/pulls/3/asc/babysit/stop")
	contexttest.LoadUser(t, ctx, user)
	ctx.IsSigned = true
	contexttest.LoadRepo(t, ctx, 1)
	ctx.SetParams(":index", "3")
	return ctx, pr
}

func TestPrepareAscBabysitPullSaysWhoWatchesAndWhoMayStop(t *testing.T) {
	for user, may := range map[int64]bool{4: true, 2: true, 1: true, 5: false} {
		ctx, pr := babysitCtx(t, user)
		prepareAscBabysitPull(ctx, pr)
		got, ok := ctx.Data["AscBabysitPull"].(*AscBabysitPull)
		require.True(t, ok, user)
		assert.Equal(t, "coder-2", got.Agent)
		assert.Equal(t, ascbabysit.MaxRuns, got.Left)
		if may {
			assert.Equal(t, "/user2/repo1/pulls/3/asc/babysit/stop", got.StopURL, user)
		} else {
			assert.Empty(t, got.StopURL, "someone who neither asked nor may change pull requests")
		}
	}

	ctx, pr := babysitCtx(t, 2)
	pr.Issue.IsClosed = true
	prepareAscBabysitPull(ctx, pr)
	assert.Nil(t, ctx.Data["AscBabysitPull"], "a closed change")

	ctx, _ = babysitCtx(t, 2)
	prepareAscBabysitPull(ctx, unittest.AssertExistsAndLoadBean(t, &issues_model.PullRequest{ID: 1}))
	assert.Nil(t, ctx.Data["AscBabysitPull"], "a change nobody watches")
}

func TestAscStopBabysitEndsTheWatchForTheRightPeople(t *testing.T) {
	ctx, _ := babysitCtx(t, 5)
	AscStopBabysit(ctx)
	assert.Equal(t, http.StatusForbidden, ctx.Resp.WrittenStatus())
	unittest.AssertExistsAndLoadBean(t, &ascbabysit.AscBabysit{PullID: 2})

	ctx, _ = babysitCtx(t, 4)
	AscStopBabysit(ctx)
	assert.Equal(t, http.StatusSeeOther, ctx.Resp.WrittenStatus())
	unittest.AssertNotExistsBean(t, &ascbabysit.AscBabysit{PullID: 2})
}

func TestAscBabysitPullTemplate(t *testing.T) {
	out := renderPart(t, "repo/pulls/asc_babysit", func(d map[string]any) {
		d["AscBabysitPull"] = &AscBabysitPull{Agent: "coder-2", Left: 2, StopURL: "/user2/repo1/pulls/3/asc/babysit/stop"}
	})
	for _, want := range []string{`data-testid="asc-babysit"`, "asc_rp.opt_babysit_on", "asc_rp.opt_babysit_left_", `action="/user2/repo1/pulls/3/asc/babysit/stop"`, `data-testid="asc-babysit-stop"`} {
		assert.Contains(t, out, want)
	}
	out = renderPart(t, "repo/pulls/asc_babysit", func(d map[string]any) { d["AscBabysitPull"] = &AscBabysitPull{Agent: "coder-2"} })
	assert.Contains(t, out, "asc_rp.opt_babysit_spent")
	assert.NotContains(t, out, "<form")
}
