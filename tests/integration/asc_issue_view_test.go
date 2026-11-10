// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package integration

import (
	"net/http"
	"strings"
	"testing"

	"forgejo.org/models/db"
	issues_model "forgejo.org/models/issues"
	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/references"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/test"
	"forgejo.org/tests"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The board notes of IssueView: the comment box keeps a draft and Cmd+Enter posts, a linked stack shows the rail, a
// landed stack that closes the issue shows the banner with Reopen, and a locked issue says who may comment.
func TestAscIssueViewBoardNotes(t *testing.T) {
	defer tests.PrepareTestEnv(t)()
	defer test.MockVariableValue(&setting.Asc.Enabled, true)()
	ctx := t.Context()
	owner := loginUser(t, "user2")

	// the comment box is a markdown editor whose textarea is named content (asc-drafts.js keeps it on the device) and
	// carries js-quick-submit (Ctrl+Enter or Cmd+Enter posts the form)
	doc := NewHTMLParser(t, owner.MakeRequest(t, NewRequest(t, "GET", "/user2/repo1/issues/1"), http.StatusOK).Body)
	box := doc.Find("#comment-form .combo-markdown-editor textarea[name=content]")
	require.Equal(t, 1, box.Length())
	assert.True(t, box.HasClass("js-quick-submit"))
	assert.Equal(t, 0, doc.Find(`[data-testid="issue-stacks"]`).Length(), "nothing names the issue yet")

	// a stack of pull 1 (#2) under pull 2 (#3); the top names issue 1 with a closing word
	pr1 := unittest.AssertExistsAndLoadBean(t, &issues_model.PullRequest{ID: 1})
	pr2 := unittest.AssertExistsAndLoadBean(t, &issues_model.PullRequest{ID: 2})
	require.NoError(t, issues_model.SetPullRequestAscStack(db.DefaultContext, pr1, "kkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkk", 0))
	require.NoError(t, issues_model.SetPullRequestAscStack(db.DefaultContext, pr2, "llllllllllllllllllllllllllllllll", pr1.ID))
	issue := unittest.AssertExistsAndLoadBean(t, &issues_model.Issue{ID: 1})
	require.NoError(t, issue.LoadRepo(ctx))
	require.NoError(t, pr2.LoadIssue(ctx))
	_, err := issues_model.CreateComment(ctx, &issues_model.CreateCommentOptions{
		Type: issues_model.CommentTypePullRef, Doer: unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2}), Repo: issue.Repo, Issue: issue,
		RefRepoID: pr2.Issue.RepoID, RefIssueID: pr2.Issue.ID, RefAction: references.XRefActionCloses, RefIsPull: true,
	})
	require.NoError(t, err)

	doc = NewHTMLParser(t, owner.MakeRequest(t, NewRequest(t, "GET", "/user2/repo1/issues/1"), http.StatusOK).Body)
	rail := doc.Find(`[data-testid="issue-stacks"]`)
	require.Equal(t, 1, rail.Length(), "the linked stack shows the rail")
	assert.Equal(t, 2, rail.Find(".asc-issue-stack-rows li").Length())
	assert.Contains(t, rail.Text(), "landed")
	href, _ := rail.Find(".asc-issue-stack > a").Attr("href")
	assert.True(t, strings.HasSuffix(href, "/pulls/3"), href)
	assert.Equal(t, 0, doc.Find(`[data-testid="closed-by-stack"]`).Length(), "the issue is open")
	assert.Positive(t, doc.Find(`.timeline .timeline-item.event a[href$="/pulls/3"]`).Length(), "the timeline has the event of the stack")

	// the stack landed and closed the issue: the banner names it and Reopen goes to the comment form
	_, err = db.GetEngine(ctx).Exec("UPDATE `pull_request` SET has_merged = ? WHERE id IN (1, 2)", true)
	require.NoError(t, err)
	_, err = db.GetEngine(ctx).Exec("UPDATE `issue` SET is_closed = ? WHERE id IN (1, 2, 3)", true)
	require.NoError(t, err)
	doc = NewHTMLParser(t, owner.MakeRequest(t, NewRequest(t, "GET", "/user2/repo1/issues/1"), http.StatusOK).Body)
	banner := doc.Find(`[data-testid="closed-by-stack"]`)
	require.Equal(t, 1, banner.Length())
	assert.Contains(t, banner.Text(), "#3 issue3")
	reopen, _ := banner.Find("a").Attr("href")
	assert.Equal(t, "#comment-form", reopen)
	assert.Equal(t, 1, doc.Find("#comment-form #status-button").Length(), "the form has the Reopen button")

	// locked: a person who may not write reads why there is no comment box
	_, err = db.GetEngine(ctx).Exec("UPDATE `issue` SET is_locked = ? WHERE id = 1", true)
	require.NoError(t, err)
	doc = NewHTMLParser(t, loginUser(t, "user8").MakeRequest(t, NewRequest(t, "GET", "/user2/repo1/issues/1"), http.StatusOK).Body)
	assert.Contains(t, doc.Find(".ds-banner-warn").Text(), "This discussion has been locked")
	assert.Equal(t, 0, doc.Find("#comment-form").Length())
}
