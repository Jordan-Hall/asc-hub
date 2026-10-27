// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package integration

import (
	"net/http"
	"testing"

	activities_model "forgejo.org/models/activities"
	"forgejo.org/models/db"
	issues_model "forgejo.org/models/issues"
	"forgejo.org/tests"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Removing a queued stack by another user is visible on the pull request and reaches the user who queued it.
func TestAscQueueTimelineAndNotificationOnRemoval(t *testing.T) {
	defer tests.PrepareTestEnv(t)()
	e := newAscQueueEnv(t)
	require.NoError(t, e.top.LoadIssue(t.Context()))
	require.NoError(t, e.bottom.LoadIssue(t.Context()))
	issueIDs := []int64{e.bottom.Issue.ID, e.top.Issue.ID}
	_, err := db.GetEngine(t.Context()).In("issue_id", issueIDs).Delete(new(activities_model.Notification))
	require.NoError(t, err)

	count := func(typ issues_model.CommentType) int64 {
		n, err := db.GetEngine(t.Context()).In("issue_id", issueIDs).And("type = ?", typ).Count(new(issues_model.Comment))
		require.NoError(t, err)
		return n
	}
	scheduled, unscheduled := count(issues_model.CommentTypePRScheduledToAutoMerge), count(issues_model.CommentTypePRUnScheduledToAutoMerge)

	owner := loginUser(t, "user2")
	owner.MakeRequest(t, NewRequestWithValues(t, "POST", "/user2/repo1/pulls/5/asc/enqueue", nil), http.StatusSeeOther)
	assert.Equal(t, scheduled+2, count(issues_model.CommentTypePRScheduledToAutoMerge), "one line for each pull request of the stack")

	// The site administrator takes the stack out of the queue.
	admin := loginUser(t, "user1")
	admin.MakeRequest(t, NewRequestWithValues(t, "POST", "/user2/repo1/pulls/5/asc/dequeue", nil), http.StatusSeeOther)
	assert.Equal(t, unscheduled+2, count(issues_model.CommentTypePRUnScheduledToAutoMerge))

	for _, id := range issueIDs {
		unittest.AssertExistsAndLoadBean(t, &activities_model.Notification{UserID: 2, IssueID: id})
		unittest.AssertNotExistsBean(t, &activities_model.Notification{UserID: 1, IssueID: id})
	}

	// The user who queued it finds the pull request in the notification list.
	resp := owner.MakeRequest(t, NewRequest(t, "GET", "/notifications"), http.StatusOK)
	assert.Contains(t, resp.Body.String(), e.top.Issue.Title)
}
