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
	owner := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2}) // owns repository 1

	none, err := AscStacksOfIssue(t.Context(), owner, issue)
	require.NoError(t, err)
	assert.Empty(t, none, "nothing names the issue")

	// the top pull request names the issue with a closing word; the second mention of the same stack is the same stack
	refer(t, issue, pr1.Issue, references.XRefActionNone)
	refer(t, issue, pr2.Issue, references.XRefActionCloses)
	stacks, err := AscStacksOfIssue(t.Context(), owner, issue)
	require.NoError(t, err)
	require.Len(t, stacks, 1, "the pull requests of one stack are one stack")
	st := stacks[0]
	assert.Len(t, st.Entries, 2)
	assert.True(t, st.Closes, "one pull request of the stack closes the issue")
	assert.Equal(t, st.Landed == len(st.Entries), st.Done())

	// the chip of a list row
	chips, err := AscIssueStackChips(t.Context(), owner, issues_model.IssueList{issue})
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
	owner := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2})
	refer(t, issue, pr.Issue, references.XRefActionCloses)
	stacks, err := AscStacksOfIssue(t.Context(), owner, issue)
	require.NoError(t, err)
	assert.Empty(t, stacks)
	chips, err := AscIssueStackChips(t.Context(), owner, issues_model.IssueList{issue})
	require.NoError(t, err)
	assert.Empty(t, chips)
}

// A pull request of a private repository that names a public issue must not show on that issue to anybody who cannot read
// the pull requests of that repository: its title and the state of its stack are private.
func TestStacksOfAPrivateRepositoryAreInvisibleToWhoCannotReadThem(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	issue := unittest.AssertExistsAndLoadBean(t, &issues_model.Issue{ID: 1}) // a public repository
	require.NoError(t, issue.LoadRepo(t.Context()))
	require.False(t, issue.Repo.IsPrivate)

	// pull request 6 belongs to repository 3 of the organization org3, which is private
	pr := unittest.AssertExistsAndLoadBean(t, &issues_model.PullRequest{ID: 6})
	require.NoError(t, pr.LoadIssue(t.Context()))
	require.NoError(t, pr.LoadBaseRepo(t.Context()))
	require.True(t, pr.BaseRepo.IsPrivate, "the fixture repository is private")
	require.NoError(t, issues_model.SetPullRequestAscStack(db.DefaultContext, pr, "kkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkk", 0))
	refer(t, issue, pr.Issue, references.XRefActionCloses)

	member := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2})    // an owner of org3
	nonMember := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 5}) // belongs to other organizations only
	admin := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 1})     // a site administrator
	list := issues_model.IssueList{issue}

	seen := func(viewer *user_model.User) (stacks, chips int, closing bool) {
		st, err := AscStacksOfIssue(t.Context(), viewer, issue)
		require.NoError(t, err)
		ch, err := AscIssueStackChips(t.Context(), viewer, list)
		require.NoError(t, err)
		closed := *issue
		closed.IsClosed = true
		merged := make([]AscIssueStack, len(st))
		copy(merged, st)
		for i := range merged {
			merged[i].Top.State = AscStackStateMerged // as if it had landed: the banner is built from what the viewer sees
		}
		return len(st), len(ch), AscClosingStackLanded(t.Context(), &closed, merged) != nil
	}

	for name, viewer := range map[string]*user_model.User{"anonymous": nil, "a person outside the organization": nonMember} {
		stacks, chips, closing := seen(viewer)
		assert.Zero(t, stacks, name)
		assert.Zero(t, chips, name)
		assert.False(t, closing, name)
	}
	for name, viewer := range map[string]*user_model.User{"a member": member, "a site administrator": admin} {
		stacks, chips, closing := seen(viewer)
		assert.Equal(t, 1, stacks, name)
		assert.Equal(t, 1, chips, name)
		assert.True(t, closing, name)
	}
}
