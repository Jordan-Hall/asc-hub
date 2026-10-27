// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package automerge

import (
	"errors"
	"testing"

	activities_model "forgejo.org/models/activities"
	"forgejo.org/models/db"
	issues_model "forgejo.org/models/issues"
	user_model "forgejo.org/models/user"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// qIssueIDs returns the issue ids of the bottom and the top pull request and clears their notifications.
func qIssueIDs(t *testing.T, f *qFixture) (bottom, top int64) {
	t.Helper()
	require.NoError(t, f.bottom.LoadIssue(t.Context()))
	require.NoError(t, f.top.LoadIssue(t.Context()))
	bottom, top = f.bottom.Issue.ID, f.top.Issue.ID
	_, err := db.GetEngine(t.Context()).In("issue_id", bottom, top).Delete(new(activities_model.Notification))
	require.NoError(t, err)
	return bottom, top
}

func qNotified(t *testing.T, userID, issueID int64) bool {
	t.Helper()
	n, err := db.GetEngine(t.Context()).Where("user_id = ? AND issue_id = ?", userID, issueID).Count(new(activities_model.Notification))
	require.NoError(t, err)
	return n > 0
}

// qLines returns the poster ids of the comments of one type on an issue.
func qLines(t *testing.T, issueID int64, typ issues_model.CommentType) []int64 {
	t.Helper()
	var cs []*issues_model.Comment
	require.NoError(t, db.GetEngine(t.Context()).Where("issue_id = ? AND type = ?", issueID, typ).OrderBy("id").Find(&cs))
	out := make([]int64, 0, len(cs))
	for _, c := range cs {
		out = append(out, c.PosterID)
	}
	return out
}

func TestAscStackQueueTimelineLines(t *testing.T) {
	f := newQFixture(t)
	bottom, top := qIssueIDs(t, f)
	before := map[int64][]int64{
		bottom: qLines(t, bottom, issues_model.CommentTypePRScheduledToAutoMerge),
		top:    qLines(t, top, issues_model.CommentTypePRScheduledToAutoMerge),
	}

	_, err := AscQueueEnqueue(t.Context(), qUser(t, 2), f.top)
	require.NoError(t, err)
	assert.Equal(t, append(before[bottom], 2), qLines(t, bottom, issues_model.CommentTypePRScheduledToAutoMerge), "a line for each pull request, as the user who queued")
	assert.Equal(t, append(before[top], 2), qLines(t, top, issues_model.CommentTypePRScheduledToAutoMerge))

	// Asking again changes nothing: the pull requests are queued already.
	_, err = AscQueueEnqueue(t.Context(), qUser(t, 2), f.top)
	require.NoError(t, err)
	assert.Len(t, qLines(t, bottom, issues_model.CommentTypePRScheduledToAutoMerge), len(before[bottom])+1)

	// An administrator removes the pull requests: the line is theirs, and the user who queued is told, and
	// the administrator is not.
	unscheduledBefore := len(qLines(t, bottom, issues_model.CommentTypePRUnScheduledToAutoMerge))
	require.NoError(t, AscQueueCancel(t.Context(), qUser(t, 1), f.bottom))
	lines := qLines(t, bottom, issues_model.CommentTypePRUnScheduledToAutoMerge)
	require.Len(t, lines, unscheduledBefore+1)
	assert.EqualValues(t, 1, lines[len(lines)-1])
	assert.True(t, qNotified(t, 2, bottom), "the user who queued it is told")
	assert.False(t, qNotified(t, 1, bottom), "the user who acted is not told about their own action")
}

func TestAscStackQueueTellsWhenTheQueueStops(t *testing.T) {
	t.Run("a cancel by the queue", func(t *testing.T) {
		f := newQFixture(t)
		bottom, top := qIssueIDs(t, f)
		_, err := AscQueueEnqueue(t.Context(), qUser(t, 2), f.top)
		require.NoError(t, err)
		before := len(qLines(t, bottom, issues_model.CommentTypePRUnScheduledToAutoMerge))

		// The user who queued it can no longer sign in: the entries are cancelled by the queue itself.
		_, err = db.GetEngine(t.Context()).ID(2).Cols("prohibit_login").Update(&user_model.User{ProhibitLogin: true})
		require.NoError(t, err)

		n, err := RunAscStackQueue(t.Context(), f.top.BaseRepoID, "master")
		require.NoError(t, err)
		assert.Zero(t, n)
		assert.Equal(t, map[int64]string{f.bottom.ID: AscQueueCancelled, f.top.ID: AscQueueCancelled}, qStates(t, f))
		lines := qLines(t, bottom, issues_model.CommentTypePRUnScheduledToAutoMerge)
		require.Len(t, lines, before+1)
		assert.EqualValues(t, 2, lines[len(lines)-1], "written as the user who queued it")
		assert.True(t, qNotified(t, 2, bottom))
		assert.True(t, qNotified(t, 2, top))
	})

	t.Run("a block waits for a new head and tells the user", func(t *testing.T) {
		f := newQFixture(t)
		bottom, _ := qIssueIDs(t, f)
		_, err := AscQueueEnqueue(t.Context(), qUser(t, 2), f.top)
		require.NoError(t, err)
		f.lander.needsCM = true
		f.lander.restackErr = errors.New("conflict")

		_, err = RunAscStackQueue(t.Context(), f.top.BaseRepoID, "master")
		require.NoError(t, err)
		assert.Equal(t, map[int64]string{f.bottom.ID: AscQueueBlocked, f.top.ID: AscQueueBlocked}, qStates(t, f))
		assert.True(t, qNotified(t, 2, bottom), "the user who queued it is told it is blocked")

		// A blocked entry is not announced again by a second run.
		_, err = db.GetEngine(t.Context()).Where("issue_id = ?", bottom).Delete(new(activities_model.Notification))
		require.NoError(t, err)
		_, err = RunAscStackQueue(t.Context(), f.top.BaseRepoID, "master")
		require.NoError(t, err)
		assert.False(t, qNotified(t, 2, bottom))
	})
}
