// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	"encoding/json"
	"net/http"
	"net/url"
	"testing"

	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/templates"
	"forgejo.org/services/contexttest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAscNewIssueDraft(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	c, changeID := ascDraftCtx(t, "")
	id := ascNewIssueDraftID(c.ctx.Repo.Repository.ID)
	assert.True(t, ascNewChangeDraftID.MatchString(id), id)
	assert.NotEqual(t, changeID, id, "the issue draft is not the draft of a change")
	assert.NotEqual(t, ascNewIssueDraftID(2), id, "each repository has its own")

	post := func(form url.Values) (int, map[string]any) {
		r, _ := ascDraftCtx(t, "")
		r.ctx.Req.Method = http.MethodPost
		r.ctx.Req.PostForm, r.ctx.Req.Form = form, form
		NewIssueDraftSave(r.ctx)
		var out map[string]any
		_ = json.Unmarshal(r.resp.Body.Bytes(), &out)
		return r.resp.Code, out
	}
	stored := func() ascNewChangeDrafts {
		raw, err := user_model.GetUserSetting(c.ctx, 2, ascNewChangeDraftKey)
		require.NoError(t, err)
		return parseAscNewChangeDrafts(raw)
	}

	// any repository keeps the draft, and the id the page sends is not read
	code, out := post(url.Values{"id": {changeID}, "title": {"Crash on start"}, "content": {"Steps\r\n1. open"}})
	assert.Equal(t, http.StatusOK, code)
	assert.Equal(t, true, out["saved"])
	require.Contains(t, stored(), id)
	assert.NotContains(t, stored(), changeID)
	assert.Equal(t, "Steps\n1. open", stored()[id].Body)

	// the new issue page comes back with it
	prepareAscNewIssueDraft(c.ctx)
	st, ok := c.ctx.Data["AscNewChangeDraft"].(*AscNewChangeDraftState)
	require.True(t, ok)
	assert.True(t, st.Issue)
	assert.True(t, st.Restored)
	assert.Equal(t, c.ctx.Repo.RepoLink+"/issues/new/draft", st.URL)
	assert.Equal(t, "Crash on start", c.ctx.Data["TitleQuery"])
	assert.Equal(t, "Steps\n1. open", c.ctx.Data["BodyQuery"])
	c.ctx.Data["TitleQuery"] = "From the address"
	prepareAscNewIssueDraft(c.ctx)
	assert.Equal(t, "From the address", c.ctx.Data["TitleQuery"], "text from the address wins")

	// and so does the dialog of the issue list
	prepareAscNewIssueDialog(c.ctx)
	d, ok := c.ctx.Data["AscIssueDialog"].(*AscNewChangeDraftState)
	require.True(t, ok)
	assert.Equal(t, "Crash on start", d.Title)
	assert.Equal(t, "Steps\n1. open", d.Body)

	// opening the issue removes the draft
	clearAscNewIssueDraft(c.ctx)
	assert.NotContains(t, stored(), id)
	prepareAscNewIssueDialog(c.ctx)
	d = c.ctx.Data["AscIssueDialog"].(*AscNewChangeDraftState)
	assert.False(t, d.Restored)
	assert.Empty(t, d.Title)

	// a discard removes it too
	post(url.Values{"title": {"again"}})
	require.Contains(t, stored(), id)
	post(url.Values{"discard": {"1"}, "title": {"ignored"}})
	assert.NotContains(t, stored(), id)
}

func TestAscNewIssueDialogOnlyWhenItCanBeUsed(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	for name, set := range map[string]func(c *ascDraftEnv){
		"issues start from a template": func(c *ascDraftEnv) { c.ctx.Data["NewIssueChooseTemplate"] = true },
		"archived":                     func(c *ascDraftEnv) { c.ctx.Repo.Repository.IsArchived = true },
		"signed out":                   func(c *ascDraftEnv) { c.ctx.Doer = nil },
	} {
		t.Run(name, func(t *testing.T) {
			c, _ := ascDraftCtx(t, "")
			set(c)
			prepareAscNewIssueDialog(c.ctx)
			assert.Nil(t, c.ctx.Data["AscIssueDialog"])
		})
	}
	c, _ := ascDraftCtx(t, "")
	c.ctx.Doer = nil
	prepareAscNewIssueDraft(c.ctx)
	assert.Nil(t, c.ctx.Data["AscNewChangeDraft"])
}

func TestAscNewIssuePagesShowTheDraft(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	c, _ := ascDraftCtx(t, "")
	d := ascNewChangeDrafts{}
	d.put(ascNewIssueDraftID(c.ctx.Repo.Repository.ID), ascNewChangeDraft{Title: "Crash on start", Body: "kept body", At: 1700000000})
	require.NoError(t, saveAscNewChangeDrafts(c.ctx, 2, d))

	t.Run("list", func(t *testing.T) {
		ctx, _ := contexttest.MockContext(t, "/user2/repo1/issues", contexttest.MockContextOption{Render: templates.HTMLRenderer()})
		contexttest.LoadUser(t, ctx, 2)
		contexttest.LoadRepo(t, ctx, 1)
		contexttest.LoadGitRepo(t, ctx)
		defer ctx.Repo.GitRepo.Close()
		prepareAscNewIssueDialog(ctx)
		require.NotNil(t, ctx.Data["AscIssueDialog"])
		ctx.Data["RepoLink"] = ctx.Repo.RepoLink // the repository middleware sets it on a real request
		out, err := ctx.RenderToHTML("repo/issue/new_dialog", ctx.Data)
		require.NoError(t, err)
		body := string(out)
		assert.Contains(t, body, `id="issue-new-dialog"`)
		assert.Contains(t, body, `action="/user2/repo1/issues/new"`)
		assert.Contains(t, body, `value="Crash on start"`)
		assert.Contains(t, body, "kept body</textarea>")
		assert.Contains(t, body, `data-asc-ncd-url="/user2/repo1/issues/new/draft"`)
		assert.Contains(t, body, `data-restored="true"`)
		assert.Contains(t, body, "asc_rp.ncd_restored")
	})
	t.Run("list handler", func(t *testing.T) {
		ctx, _ := contexttest.MockContext(t, "/user2/repo1/issues")
		contexttest.LoadUser(t, ctx, 2)
		contexttest.LoadRepo(t, ctx, 1)
		contexttest.LoadGitRepo(t, ctx)
		defer ctx.Repo.GitRepo.Close()
		Issues(ctx)
		d, ok := ctx.Data["AscIssueDialog"].(*AscNewChangeDraftState)
		require.True(t, ok, "the issue list offers the dialog")
		assert.Equal(t, "Crash on start", d.Title)
	})
	t.Run("new issue page", func(t *testing.T) {
		ctx, _ := contexttest.MockContext(t, "/user2/repo1/issues/new")
		contexttest.LoadUser(t, ctx, 2)
		contexttest.LoadRepo(t, ctx, 1)
		contexttest.LoadGitRepo(t, ctx)
		defer ctx.Repo.GitRepo.Close()
		NewIssue(ctx)
		st, ok := ctx.Data["AscNewChangeDraft"].(*AscNewChangeDraftState)
		require.True(t, ok, "the new issue page keeps a draft")
		assert.Equal(t, "/user2/repo1/issues/new/draft", st.URL)
		assert.Equal(t, "Crash on start", ctx.Data["TitleQuery"])
		assert.Equal(t, "kept body", ctx.Data["BodyQuery"])
	})
}
