// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package pull

import (
	"testing"

	"forgejo.org/models/db"
	issues_model "forgejo.org/models/issues"
	project_model "forgejo.org/models/project"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/references"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// boardStack makes pull 1 (issue 2, merged) and pull 2 (issue 3, open) of repository 1 a stack, and pull 2 close issue 1.
// Project 1 of repository 1 holds the cards of issues 1, 2, 3 and 5.
func boardStack(t *testing.T) (pr1, pr2 *issues_model.PullRequest) {
	t.Helper()
	require.NoError(t, unittest.PrepareTestDatabase())
	pr1 = unittest.AssertExistsAndLoadBean(t, &issues_model.PullRequest{ID: 1})
	pr2 = unittest.AssertExistsAndLoadBean(t, &issues_model.PullRequest{ID: 2})
	require.NoError(t, pr1.LoadIssue(t.Context()))
	require.NoError(t, pr2.LoadIssue(t.Context()))
	require.NoError(t, issues_model.SetPullRequestAscStack(db.DefaultContext, pr1, "kkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkk", 0))
	require.NoError(t, issues_model.SetPullRequestAscStack(db.DefaultContext, pr2, "llllllllllllllllllllllllllllllll", pr1.ID))
	refer(t, unittest.AssertExistsAndLoadBean(t, &issues_model.Issue{ID: 1}), pr2.Issue, references.XRefActionCloses)
	return pr1, pr2
}

func TestACardOfTheBoardFollowsItsStack(t *testing.T) {
	_, pr2 := boardStack(t)
	owner := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2})
	issue1 := unittest.AssertExistsAndLoadBean(t, &issues_model.Issue{ID: 1})
	issue3 := unittest.AssertExistsAndLoadBean(t, &issues_model.Issue{ID: 3})
	issue5 := unittest.AssertExistsAndLoadBean(t, &issues_model.Issue{ID: 5})

	stacks, err := AscProjectCardStacks(t.Context(), owner, map[int64]issues_model.IssueList{1: {issue1, issue5}, 2: {issue3}})
	require.NoError(t, err)
	require.Contains(t, stacks, int64(1))
	require.Contains(t, stacks, int64(3))
	assert.NotContains(t, stacks, int64(5), "nothing names issue 5")

	pull := stacks[3]
	assert.Len(t, pull.Entries, 2)
	assert.Equal(t, 1, pull.Landed, "the bottom has merged")
	assert.Equal(t, pr2.Issue.Index, pull.Top.Index, "the card of a pull request links to itself")
	assert.False(t, pull.Done())

	issue := stacks[1]
	assert.True(t, issue.Closes, "the card of an issue shows the stack that fixes it")
	assert.Equal(t, 1, issue.Landed)

	// it follows the state: once the top merges, the stack has landed
	pr2.HasMerged = true
	_, err = db.GetEngine(t.Context()).ID(pr2.ID).Cols("has_merged").Update(pr2)
	require.NoError(t, err)
	stacks, err = AscProjectCardStacks(t.Context(), owner, map[int64]issues_model.IssueList{2: {issue3}})
	require.NoError(t, err)
	assert.True(t, stacks[3].Done())

	// a pull request of a private repository is not shown to a viewer who may not read it
	_, err = db.GetEngine(t.Context()).ID(1).Cols("is_private").Update(&repo_model.Repository{IsPrivate: true})
	require.NoError(t, err)
	stacks, err = AscProjectCardStacks(t.Context(), nil, map[int64]issues_model.IssueList{1: {issue1}, 2: {issue3}})
	require.NoError(t, err)
	assert.Empty(t, stacks)
}

func TestALandedStackMovesItsCardsToTheLandedColumn(t *testing.T) {
	_, pr2 := boardStack(t)

	n, err := MoveAscLandedCards(t.Context(), pr2)
	require.NoError(t, err)
	assert.Zero(t, n, "the top has not merged")

	pr2.HasMerged = true
	_, err = db.GetEngine(t.Context()).ID(pr2.ID).Cols("has_merged").Update(pr2)
	require.NoError(t, err)
	n, err = MoveAscLandedCards(t.Context(), pr2)
	require.NoError(t, err)
	assert.Zero(t, n, "the project has no Landed column")

	require.NoError(t, project_model.SetLandedColumn(t.Context(), 1, 3, true))
	n, err = MoveAscLandedCards(t.Context(), pr2)
	require.NoError(t, err)
	assert.Equal(t, 3, n)
	for issueID, sorting := range map[int64]int64{5: 0, 2: 1, 3: 2, 1: 3} {
		card := unittest.AssertExistsAndLoadBean(t, &project_model.ProjectIssue{ProjectID: 1, IssueID: issueID})
		assert.EqualValues(t, 3, card.ProjectColumnID, issueID)
		assert.Equal(t, sorting, card.Sorting, "the stack bottom to top, then the issue it closes: %d", issueID)
	}

	n, err = MoveAscLandedCards(t.Context(), pr2)
	require.NoError(t, err)
	assert.Zero(t, n, "a card in the Landed column stays")
}

func TestAStackThatHasNotLandedWholeMovesNoCard(t *testing.T) {
	pr1, _ := boardStack(t)
	require.NoError(t, project_model.SetLandedColumn(t.Context(), 1, 3, true))
	n, err := MoveAscLandedCards(t.Context(), pr1)
	require.NoError(t, err)
	assert.Zero(t, n, "the bottom merged, the top is open")
	card := unittest.AssertExistsAndLoadBean(t, &project_model.ProjectIssue{ProjectID: 1, IssueID: 3})
	assert.EqualValues(t, 2, card.ProjectColumnID)
}

func TestAPullRequestThatIsNotStackedLandsAlone(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	pr1 := unittest.AssertExistsAndLoadBean(t, &issues_model.PullRequest{ID: 1}) // merged, not stacked
	require.NoError(t, project_model.SetLandedColumn(t.Context(), 1, 3, true))
	n, err := MoveAscLandedCards(t.Context(), pr1)
	require.NoError(t, err)
	assert.Equal(t, 1, n)
	card := unittest.AssertExistsAndLoadBean(t, &project_model.ProjectIssue{ProjectID: 1, IssueID: 2})
	assert.EqualValues(t, 3, card.ProjectColumnID)
}
