// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package pull

import (
	"testing"

	"forgejo.org/models/db"
	issues_model "forgejo.org/models/issues"
	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/references"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// refer makes the comment that a pull request leaves on an issue it names.
func refer(t *testing.T, issue, prIssue *issues_model.Issue, action references.XRefAction) {
	t.Helper()
	doer := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2})
	require.NoError(t, issue.LoadRepo(t.Context()))
	_, err := issues_model.CreateComment(t.Context(), &issues_model.CreateCommentOptions{
		Type: issues_model.CommentTypePullRef, Doer: doer, Repo: issue.Repo, Issue: issue,
		RefRepoID: prIssue.RepoID, RefIssueID: prIssue.ID, RefAction: action, RefIsPull: true,
	})
	require.NoError(t, err)
}

func TestStacksNamingAnIssue(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	// a stack of two pull requests of repository 1: issue 2 (pull 1) under issue 3 (pull 2)? use the pulls of repository 1
	pr1 := unittest.AssertExistsAndLoadBean(t, &issues_model.PullRequest{ID: 1})
	pr2 := unittest.AssertExistsAndLoadBean(t, &issues_model.PullRequest{ID: 2})
	require.NoError(t, pr1.LoadIssue(t.Context()))
	require.NoError(t, pr2.LoadIssue(t.Context()))
	require.NoError(t, issues_model.SetPullRequestAscStack(db.DefaultContext, pr1, "kkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkk", 0))
	require.NoError(t, issues_model.SetPullRequestAscStack(db.DefaultContext, pr2, "llllllllllllllllllllllllllllllll", pr1.ID))

	issue := unittest.AssertExistsAndLoadBean(t, &issues_model.Issue{ID: 1})
	require.False(t, issue.IsPull)

	none, err := AscStacksOfIssue(t.Context(), issue)
	require.NoError(t, err)
	assert.Empty(t, none, "nothing names the issue")

	// the top pull request names the issue with a closing word; the second mention of the same stack is the same stack
	refer(t, issue, pr1.Issue, references.XRefActionNone)
	refer(t, issue, pr2.Issue, references.XRefActionCloses)
	stacks, err := AscStacksOfIssue(t.Context(), issue)
	require.NoError(t, err)
	require.Len(t, stacks, 1, "the pull requests of one stack are one stack")
	st := stacks[0]
	assert.Len(t, st.Entries, 2)
	assert.True(t, st.Closes, "one pull request of the stack closes the issue")
	assert.Equal(t, st.Landed == len(st.Entries), st.Done())

	// the chip of a list row
	chips, err := AscIssueStackChips(t.Context(), issues_model.IssueList{issue})
	require.NoError(t, err)
	require.Contains(t, chips, issue.ID)
	assert.True(t, chips[issue.ID].Closes)

	// "closed by a landed stack": needs a closed issue and a merged closing pull request
	assert.Nil(t, AscClosingStackLanded(t.Context(), issue, stacks), "the issue is open")
	closed := *issue
	closed.IsClosed = true
	landed := []AscIssueStack{{Closes: true, Top: AscStackEntry{State: AscStackStateMerged, Index: 7}}}
	assert.NotNil(t, AscClosingStackLanded(t.Context(), &closed, landed))
	landed[0].Top.State = AscStackStateOpen
	assert.Nil(t, AscClosingStackLanded(t.Context(), &closed, landed), "the stack has not landed")
	landed[0].Top.State, landed[0].Closes = AscStackStateMerged, false
	assert.Nil(t, AscClosingStackLanded(t.Context(), &closed, landed), "the stack only mentions the issue")
}

func TestStacksNamingAnIssueIgnoresPullsThatAreNotStacked(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	pr := unittest.AssertExistsAndLoadBean(t, &issues_model.PullRequest{ID: 1})
	require.NoError(t, pr.LoadIssue(t.Context()))
	issue := unittest.AssertExistsAndLoadBean(t, &issues_model.Issue{ID: 1})
	refer(t, issue, pr.Issue, references.XRefActionCloses)
	stacks, err := AscStacksOfIssue(t.Context(), issue)
	require.NoError(t, err)
	assert.Empty(t, stacks)
	chips, err := AscIssueStackChips(t.Context(), issues_model.IssueList{issue})
	require.NoError(t, err)
	assert.Empty(t, chips)
}
