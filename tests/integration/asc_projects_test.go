// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package integration

import (
	"net/http"
	"strconv"
	"testing"

	"forgejo.org/models/db"
	issues_model "forgejo.org/models/issues"
	"forgejo.org/models/unittest"
	"forgejo.org/tests"

	"github.com/PuerkitoBio/goquery"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Cards on a project board link to their issue or pull request, and a card that follows a stack links to the stack.
func TestAscProjectCardsLinkToTheStackOrTheIssue(t *testing.T) {
	defer tests.PrepareTestEnv(t)()
	sess := loginUser(t, "user2")

	// pull 2 (#3) stacks on pull 1 (#2): their cards follow the stack, and the chip opens the stack on the pull itself
	pr1 := unittest.AssertExistsAndLoadBean(t, &issues_model.PullRequest{ID: 1})
	pr2 := unittest.AssertExistsAndLoadBean(t, &issues_model.PullRequest{ID: 2})
	require.NoError(t, issues_model.SetPullRequestAscStack(db.DefaultContext, pr1, "kkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkk", 0))
	require.NoError(t, issues_model.SetPullRequestAscStack(db.DefaultContext, pr2, "llllllllllllllllllllllllllllllll", pr1.ID))

	resp := sess.MakeRequest(t, NewRequest(t, "GET", "/user2/repo1/projects/1"), http.StatusOK)
	cards := NewHTMLParser(t, resp.Body).Find("#project-board .issue-card")
	require.Positive(t, cards.Length())
	var sawIssue, sawStack bool
	cards.Each(func(_ int, card *goquery.Selection) {
		id, err := strconv.ParseInt(card.AttrOr("data-issue", ""), 10, 64)
		require.NoError(t, err)
		issue := unittest.AssertExistsAndLoadBean(t, &issues_model.Issue{ID: id})
		require.NoError(t, issue.LoadRepo(db.DefaultContext))
		assert.Equal(t, issue.Link(), card.Find("a.issue-card-title").AttrOr("href", ""), "the title opens the issue or the pull request")
		if chip := card.Find(`[data-testid="project-card-stack"]`); chip.Length() > 0 {
			sawStack = true
			assert.True(t, issue.IsPull)
			assert.Equal(t, issue.Link()+"#asc-stack-title", chip.AttrOr("href", ""), "the chip opens the stack")
		} else if !issue.IsPull {
			sawIssue = true
		}
	})
	assert.True(t, sawIssue)
	assert.True(t, sawStack)
}
