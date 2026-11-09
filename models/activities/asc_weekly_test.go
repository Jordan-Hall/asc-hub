// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package activities_test

import (
	"testing"
	"time"

	activities_model "forgejo.org/models/activities"
	"forgejo.org/models/db"
	issues_model "forgejo.org/models/issues"
	"forgejo.org/models/unittest"
	"forgejo.org/modules/timeutil"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTheWeeklyActivityOfTheDirectories(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	ctx := t.Context()
	e := db.GetEngine(ctx)
	now := time.Now()
	ago := func(d time.Duration) timeutil.TimeStamp { return timeutil.TimeStamp(now.Add(-d).Unix()) }
	day := 24 * time.Hour
	act := func(userID, actUserID, repoID int64, private bool, when timeutil.TimeStamp) {
		_, err := e.NoAutoTime().Insert(&activities_model.Action{
			UserID: userID, ActUserID: actUserID, RepoID: repoID, OpType: activities_model.ActionCreateIssue,
			IsPrivate: private, CreatedUnix: when,
		})
		require.NoError(t, err)
	}
	// user2 in repo1 (public, of user2): two this week, one three weeks ago, one before the window
	act(2, 2, 1, false, ago(time.Hour))
	act(2, 2, 1, false, ago(2*day))
	act(2, 2, 1, false, ago(3*7*day+day))
	act(2, 2, 1, false, ago(13*7*day))
	// the copy in the feed of a watcher, a private action and an action in a private repository do not count
	act(5, 2, 1, false, ago(time.Hour))
	act(2, 2, 1, true, ago(time.Hour))
	act(2, 2, 2, false, ago(time.Hour))
	// org17: one in its public repo23, none in its private repo24; privated_org has a public repository but a private profile
	act(5, 5, 23, false, ago(30*time.Minute))
	act(5, 5, 24, false, ago(time.Minute))
	act(5, 5, 40, false, ago(time.Minute))

	t.Run("People", func(t *testing.T) {
		// user1 opened the pull requests 2 and 3 of repo1 this week; the pull request 4 of the private repo2 is not counted
		_, err := e.Exec("UPDATE `issue` SET created_unix = ? WHERE id IN (2, 3)", ago(day))
		require.NoError(t, err)
		_, err = e.Exec("UPDATE `issue` SET is_pull = ?, created_unix = ? WHERE id = 4", true, ago(day))
		require.NoError(t, err)
		for _, r := range []*issues_model.Review{
			{Type: issues_model.ReviewTypeApprove, ReviewerID: 5, IssueID: 2, CreatedUnix: ago(day)},
			{Type: issues_model.ReviewTypeReject, ReviewerID: 5, IssueID: 3, CreatedUnix: ago(2 * day)},
			{Type: issues_model.ReviewTypePending, ReviewerID: 5, IssueID: 2, CreatedUnix: ago(day)},
			{Type: issues_model.ReviewTypeApprove, ReviewerID: 5, IssueID: 4, CreatedUnix: ago(day)},
			{Type: issues_model.ReviewTypeApprove, ReviewerID: 5, IssueID: 2, CreatedUnix: ago(20 * 7 * day)},
		} {
			_, err := e.NoAutoTime().Insert(r)
			require.NoError(t, err)
		}

		got, err := activities_model.AscUserActivity(ctx, []int64{1, 2, 5}, now)
		require.NoError(t, err)
		require.Len(t, got[2].Weeks, activities_model.AscActivityWeeks)
		want := make([]uint32, activities_model.AscActivityWeeks)
		want[11], want[8] = 2, 1
		assert.Equal(t, want, got[2].Weeks)
		assert.EqualValues(t, 3, got[2].Total)
		assert.EqualValues(t, 2, got[1].Changesets)
		assert.EqualValues(t, 0, got[2].Changesets, "a pull request in a private repository is not counted")
		assert.EqualValues(t, 2, got[5].Reviews, "pending reviews, private repositories and old reviews are not counted")
		assert.EqualValues(t, 0, got[1].Reviews)
		assert.Zero(t, got[1].Total)
	})

	t.Run("Organizations", func(t *testing.T) {
		got, err := activities_model.AscOrgActivity(ctx, []int64{17, 23, 19}, now)
		require.NoError(t, err)
		assert.EqualValues(t, 1, got[17].Total)
		assert.EqualValues(t, 1, got[17].Weeks[11])
		assert.Equal(t, ago(30*time.Minute), got[17].Last, "the latest action anyone may see")
		assert.Zero(t, got[23].Total, "an organization with a private profile shows nothing")
		assert.Zero(t, got[23].Last)
		assert.Zero(t, got[19].Total)
	})

	t.Run("An organization does not show the actions of a person who keeps activity private", func(t *testing.T) {
		act(8, 8, 23, false, ago(time.Minute))
		_, err := e.Exec("UPDATE `user` SET keep_activity_private = ? WHERE id = 8", true)
		require.NoError(t, err)
		got, err := activities_model.AscOrgActivity(ctx, []int64{17}, now)
		require.NoError(t, err)
		assert.EqualValues(t, 1, got[17].Total)
		assert.Equal(t, ago(30*time.Minute), got[17].Last)
	})

	t.Run("No accounts", func(t *testing.T) {
		got, err := activities_model.AscUserActivity(ctx, nil, now)
		require.NoError(t, err)
		assert.Empty(t, got)
	})
}
