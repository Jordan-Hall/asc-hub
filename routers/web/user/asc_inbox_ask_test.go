// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package user

import (
	gocontext "context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"forgejo.org/models/db"
	issues_model "forgejo.org/models/issues"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/templates"
	"forgejo.org/modules/test"
	"forgejo.org/services/asctrust"
	"forgejo.org/services/context"
	"forgejo.org/services/contexttest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func postAscInboxAsk(t *testing.T, doer *user_model.User, form map[string]string, accept string) (*context.Context, int, string, string) {
	t.Helper()
	ctx, resp := contexttest.MockContext(t, "POST /-/asc/inbox/ask")
	ctx.Render = templates.HTMLRenderer()
	ctx.Doer = doer
	ctx.IsSigned = doer != nil
	if accept != "" {
		ctx.Req.Header.Set("Accept", accept)
	}
	for k, v := range form {
		ctx.Req.Form.Set(k, v)
	}
	AscInboxAsk(ctx)
	return ctx, resp.Code, resp.Header().Get("Location"), resp.Body.String()
}

type ascInboxAskFake struct {
	agent    string
	runErr   error
	cmtErr   error
	runs     []*ascv1.CreateAgentRunRequest
	repos    []string
	comments []string
}

func useAscInboxAskFake(t *testing.T, f *ascInboxAskFake) {
	t.Helper()
	oldAgent, oldRun, oldCmt := ascInboxAskAgent, ascInboxAskRun, ascInboxAskComment
	ascInboxAskAgent = func(gocontext.Context, *user_model.User, *AscInboxEntry) string { return f.agent }
	ascInboxAskRun = func(_ gocontext.Context, _ *user_model.User, repo *repo_model.Repository, req *ascv1.CreateAgentRunRequest) error {
		f.runs = append(f.runs, req)
		f.repos = append(f.repos, repo.FullName())
		return f.runErr
	}
	ascInboxAskComment = func(_ gocontext.Context, _ *user_model.User, _ *issues_model.PullRequest, text string) error {
		f.comments = append(f.comments, text)
		return f.cmtErr
	}
	t.Cleanup(func() { ascInboxAskAgent, ascInboxAskRun, ascInboxAskComment = oldAgent, oldRun, oldCmt })
}

func TestAscInboxAskTask(t *testing.T) {
	got := ascInboxAskTask("user2", 3, "rename the flag")
	assert.Contains(t, got, "#3")
	assert.Contains(t, got, "INSTRUCTION (from user2, who may change this pull request):\n```\nrename the flag\n```")

	got = ascInboxAskTask("user2", 3, "use ```go\nx := 1\n``` and ````")
	assert.True(t, strings.HasSuffix(got, "\n`````"), "the fence is longer than any run of backticks in the text")
	assert.Contains(t, got, ":\n`````\nuse ```go")
}

func TestAscInboxAskAgentFromModels(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	ctx := db.DefaultContext
	user2 := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2})
	user5 := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 5})
	pr := unittest.AssertExistsAndLoadBean(t, &issues_model.PullRequest{ID: 2})

	assert.Empty(t, ascInboxAskAgentFromModels(ctx, user2, &AscInboxEntry{PullID: 2}), "no agent made it")
	require.NoError(t, issues_model.SetPullRequestAscRun(ctx, pr, 7, "builder", "user2"))
	assert.Equal(t, "builder", ascInboxAskAgentFromModels(ctx, user2, &AscInboxEntry{PullID: 2}), "the owner may ask")
	assert.Empty(t, ascInboxAskAgentFromModels(ctx, user5, &AscInboxEntry{PullID: 2}), "a person who may not change it")
	assert.Empty(t, ascInboxAskAgentFromModels(ctx, nil, &AscInboxEntry{PullID: 2}))
	assert.Empty(t, ascInboxAskAgentFromModels(ctx, user2, &AscInboxEntry{PullID: 99999}))

	merged := unittest.AssertExistsAndLoadBean(t, &issues_model.PullRequest{ID: 1})
	require.NoError(t, issues_model.SetPullRequestAscRun(ctx, merged, 8, "builder", "user2"))
	assert.Empty(t, ascInboxAskAgentFromModels(ctx, user2, &AscInboxEntry{PullID: 1}), "a merged pull request")

	fork := unittest.AssertExistsAndLoadBean(t, &issues_model.PullRequest{ID: 3})
	require.NoError(t, issues_model.SetPullRequestAscRun(ctx, fork, 9, "builder", "user2"))
	assert.Empty(t, ascInboxAskAgentOf(ctx, fork), "its branch is in another repository")
}

func TestAscInboxAskButton(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	user2 := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2})
	defer test.MockVariableValue(&setting.Asc.Enabled, true)()
	useAscInboxEntries(t, &AscInboxEntry{PullID: 2, Title: "t", Link: "/user2/repo1/pulls/3", RepoFullName: "user2/repo1", Index: 3, Section: issues_model.AscInboxNeedsReview})

	f := &ascInboxAskFake{agent: "builder"}
	useAscInboxAskFake(t, f)
	_, body := getAscInbox(t, user2, "?pull=2")
	assert.Contains(t, body, `formaction="/-/asc/inbox/ask"`)
	assert.Contains(t, body, "data-asc-ask-agent")
	assert.Contains(t, body, "user.asc_inbox.ask_agent")
	assert.Contains(t, body, `<input type="hidden" name="pull" value="2">`)

	f.agent = ""
	_, body = getAscInbox(t, user2, "?pull=2")
	assert.NotContains(t, body, "data-asc-ask-agent")
}

func TestAscInboxAskPost(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	user2 := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2})
	defer test.MockVariableValue(&setting.Asc.Enabled, true)()
	useAscInboxEntries(t, &AscInboxEntry{PullID: 2, Title: "t", Section: issues_model.AscInboxNeedsReview})
	form := func(content string) map[string]string {
		return map[string]string{"pull": "2", "tab": "waiting", "content": content}
	}

	t.Run("asks the agent and posts the comment", func(t *testing.T) {
		f := &ascInboxAskFake{agent: "builder"}
		useAscInboxAskFake(t, f)
		ctx, code, loc, _ := postAscInboxAsk(t, user2, form("  rename the flag  "), "text/html")
		assert.Equal(t, http.StatusSeeOther, code)
		assert.Equal(t, "/-/asc/inbox?pull=2&tab=waiting", loc)
		require.Len(t, f.runs, 1)
		assert.Equal(t, "builder", f.runs[0].Agent)
		assert.Equal(t, "branch2", f.runs[0].Target)
		assert.Equal(t, "Change #3 as asked in review", f.runs[0].Title)
		assert.Contains(t, f.runs[0].Task, "\nrename the flag\n")
		assert.Equal(t, []string{"user2/repo1"}, f.repos)
		assert.Equal(t, []string{"rename the flag"}, f.comments)
		assert.NotEmpty(t, ctx.Flash.SuccessMsg)
	})

	t.Run("the fetch of the page gets a JSON redirect", func(t *testing.T) {
		useAscInboxAskFake(t, &ascInboxAskFake{agent: "builder"})
		_, code, _, body := postAscInboxAsk(t, user2, form("x"), "")
		assert.Equal(t, http.StatusOK, code)
		assert.JSONEq(t, `{"redirect":"/-/asc/inbox?pull=2&tab=waiting"}`, body)
	})

	t.Run("an empty or too long text asks nothing", func(t *testing.T) {
		f := &ascInboxAskFake{agent: "builder"}
		useAscInboxAskFake(t, f)
		ctx, _, _, _ := postAscInboxAsk(t, user2, form("   "), "text/html")
		assert.NotEmpty(t, ctx.Flash.ErrorMsg)
		ctx, _, _, _ = postAscInboxAsk(t, user2, form(strings.Repeat("a", ascInboxAskMax+1)), "text/html")
		assert.NotEmpty(t, ctx.Flash.ErrorMsg)
		assert.Empty(t, f.runs)
		assert.Empty(t, f.comments)
	})

	t.Run("no agent to ask, or asc refuses", func(t *testing.T) {
		f := &ascInboxAskFake{}
		useAscInboxAskFake(t, f)
		_, code, _, _ := postAscInboxAsk(t, user2, form("x"), "text/html")
		assert.Equal(t, http.StatusForbidden, code)
		assert.Empty(t, f.runs)

		f = &ascInboxAskFake{agent: "builder", runErr: asctrust.ErrForbidden}
		useAscInboxAskFake(t, f)
		_, code, _, _ = postAscInboxAsk(t, user2, form("x"), "text/html")
		assert.Equal(t, http.StatusForbidden, code)
		assert.Empty(t, f.comments, "no comment without a run")
	})

	t.Run("a run that fails posts no comment", func(t *testing.T) {
		f := &ascInboxAskFake{agent: "builder", runErr: errors.New("down")}
		useAscInboxAskFake(t, f)
		ctx, code, _, _ := postAscInboxAsk(t, user2, form("x"), "text/html")
		assert.Equal(t, http.StatusSeeOther, code)
		assert.NotEmpty(t, ctx.Flash.ErrorMsg)
		assert.Empty(t, f.comments)
	})

	t.Run("a comment that fails warns", func(t *testing.T) {
		f := &ascInboxAskFake{agent: "builder", cmtErr: errors.New("locked")}
		useAscInboxAskFake(t, f)
		ctx, code, _, _ := postAscInboxAsk(t, user2, form("x"), "text/html")
		assert.Equal(t, http.StatusSeeOther, code)
		assert.Len(t, f.runs, 1)
		assert.NotEmpty(t, ctx.Flash.WarningMsg)
		assert.Empty(t, ctx.Flash.SuccessMsg)
	})

	t.Run("refused requests", func(t *testing.T) {
		f := &ascInboxAskFake{agent: "builder"}
		useAscInboxAskFake(t, f)
		_, code, _, _ := postAscInboxAsk(t, user2, map[string]string{"pull": "x", "content": "x"}, "")
		assert.Equal(t, http.StatusBadRequest, code)
		_, code, _, _ = postAscInboxAsk(t, user2, map[string]string{"pull": "1", "content": "x"}, "")
		assert.Equal(t, http.StatusNotFound, code, "not in the inbox")
		_, code, _, _ = postAscInboxAsk(t, nil, form("x"), "")
		assert.Equal(t, http.StatusNotFound, code)
		defer test.MockVariableValue(&setting.Asc.Enabled, false)()
		_, code, _, _ = postAscInboxAsk(t, user2, form("x"), "")
		assert.Equal(t, http.StatusNotFound, code)
		assert.Empty(t, f.runs)
	})
}
