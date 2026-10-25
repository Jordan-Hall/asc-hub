// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package issues_test

import (
	"testing"
	"time"

	"forgejo.org/models/db"
	issues_model "forgejo.org/models/issues"
	"forgejo.org/models/unittest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAscInboxStateSection(t *testing.T) {
	const author, rev, other = int64(1), int64(2), int64(3)
	head := "h2"
	facts := func(mod func(*issues_model.AscInboxFacts)) *issues_model.AscInboxFacts {
		f := &issues_model.AscInboxFacts{HeadCommitID: head, AuthorID: author}
		mod(f)
		return f
	}
	cases := []struct {
		name string
		f    *issues_model.AscInboxFacts
		user int64
		want issues_model.AscInboxSection
	}{
		{"requested reviewer", facts(func(f *issues_model.AscInboxFacts) {
			f.Reviewers = []issues_model.AscInboxReviewer{{UserID: rev, Type: issues_model.ReviewTypeRequest}}
		}), rev, issues_model.AscInboxNeedsReview},
		{"reviewer approved head waits", facts(func(f *issues_model.AscInboxFacts) {
			f.Reviewers = []issues_model.AscInboxReviewer{{UserID: rev, Type: issues_model.ReviewTypeApprove, CommitID: head}}
		}), rev, issues_model.AscInboxWaiting},
		{"rejected then new version returns to reviewer", facts(func(f *issues_model.AscInboxFacts) {
			f.Reviewers = []issues_model.AscInboxReviewer{{UserID: rev, Type: issues_model.ReviewTypeReject, CommitID: "h1"}}
		}), rev, issues_model.AscInboxNeedsReview},
		{"rejected at head is the author's turn", facts(func(f *issues_model.AscInboxFacts) {
			f.Reviewers = []issues_model.AscInboxReviewer{{UserID: rev, Type: issues_model.ReviewTypeReject, CommitID: head}}
		}), author, issues_model.AscInboxReturned},
		{"rejected at head waits for reviewer-side", facts(func(f *issues_model.AscInboxFacts) {
			f.Reviewers = []issues_model.AscInboxReviewer{{UserID: rev, Type: issues_model.ReviewTypeReject, CommitID: head}}
		}), rev, issues_model.AscInboxWaiting},
		{"rejected on an old version, author waits", facts(func(f *issues_model.AscInboxFacts) {
			f.Reviewers = []issues_model.AscInboxReviewer{{UserID: rev, Type: issues_model.ReviewTypeReject, CommitID: "h1"}}
		}), author, issues_model.AscInboxWaiting},
		{"approved is ready", facts(func(f *issues_model.AscInboxFacts) { f.Approved = true }), author, issues_model.AscInboxReady},
		{"approved with failing checks is blocked", facts(func(f *issues_model.AscInboxFacts) { f.Approved, f.ChecksFailing = true, true }), author, issues_model.AscInboxBlocked},
		{"conflict is blocked", facts(func(f *issues_model.AscInboxFacts) { f.Conflicted = true }), author, issues_model.AscInboxBlocked},
		{"draft", facts(func(f *issues_model.AscInboxFacts) { f.Draft, f.Approved = true, true }), author, issues_model.AscInboxDrafts},
		{"queued", facts(func(f *issues_model.AscInboxFacts) { f.Queued, f.Approved = true, true }), author, issues_model.AscInboxQueued},
		{"merged", facts(func(f *issues_model.AscInboxFacts) { f.Merged = true }), author, issues_model.AscInboxMerged},
		{"closed unmerged", facts(func(f *issues_model.AscInboxFacts) { f.Closed = true }), author, issues_model.AscInboxNone},
		{"uninvolved user", facts(func(f *issues_model.AscInboxFacts) { f.Approved = true }), other, issues_model.AscInboxNone},
		{"anonymous", facts(func(f *issues_model.AscInboxFacts) {}), 0, issues_model.AscInboxNone},
		{"draft is no reviewer's turn", facts(func(f *issues_model.AscInboxFacts) {
			f.Draft = true
			f.Reviewers = []issues_model.AscInboxReviewer{{UserID: rev, Type: issues_model.ReviewTypeRequest}}
		}), rev, issues_model.AscInboxWaiting},
	}
	for _, c := range cases {
		assert.Equal(t, c.want, issues_model.AscInboxSectionFor(c.f, c.user), c.name)
	}
	assert.Equal(t, issues_model.AscInboxNone, issues_model.AscInboxSectionFor(nil, 1))
	assert.True(t, issues_model.AscInboxNeedsReview.IsValid())
	assert.False(t, issues_model.AscInboxNone.IsValid())
	assert.False(t, issues_model.AscInboxSection("x").IsValid())
}

func TestAscInboxStatePinSnooze(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	ctx := t.Context()
	now := time.Unix(1_800_000_000, 0)
	yes := true
	until := now.Add(time.Hour)

	s, err := issues_model.SetAscInboxState(ctx, 1, 10, &yes, &until, now)
	require.NoError(t, err)
	assert.True(t, s.Pinned)
	assert.True(t, s.IsSnoozed(now))
	assert.False(t, s.IsSnoozed(now.Add(2*time.Hour)))

	// Updating one mark leaves the other.
	no := false
	s, err = issues_model.SetAscInboxState(ctx, 1, 10, &no, nil, now)
	require.NoError(t, err)
	assert.False(t, s.Pinned)
	assert.True(t, s.IsSnoozed(now))
	assert.Equal(t, 1, unittest.GetCount(t, &issues_model.AscInboxState{UserID: 1, PullID: 10}))

	// A snooze in the past clears it, and an empty state deletes the row.
	past := now.Add(-time.Minute)
	_, err = issues_model.SetAscInboxState(ctx, 1, 10, nil, &past, now)
	require.NoError(t, err)
	got, err := issues_model.GetAscInboxState(ctx, 1, 10)
	require.NoError(t, err)
	assert.Nil(t, got)

	// A snooze beyond the horizon is refused.
	far := now.Add(issues_model.AscInboxMaxSnooze + time.Hour)
	_, err = issues_model.SetAscInboxState(ctx, 1, 10, nil, &far, now)
	require.ErrorIs(t, err, issues_model.ErrAscInboxBadSnooze)

	// Bad keys are refused.
	_, err = issues_model.SetAscInboxState(ctx, 0, 10, &yes, nil, now)
	require.Error(t, err)
	_, err = issues_model.SetAscInboxState(ctx, 1, 0, &yes, nil, now)
	require.Error(t, err)
}

// A user's marks are private to that user.
func TestAscInboxStatePrivate(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	ctx := t.Context()
	now := time.Unix(1_800_000_000, 0)
	yes := true
	_, err := issues_model.SetAscInboxState(ctx, 1, 10, &yes, nil, now)
	require.NoError(t, err)

	other, err := issues_model.GetAscInboxState(ctx, 2, 10)
	require.NoError(t, err)
	assert.Nil(t, other)

	m, err := issues_model.ListAscInboxStates(ctx, 2, []int64{10, 11})
	require.NoError(t, err)
	assert.Empty(t, m)
	m, err = issues_model.ListAscInboxStates(ctx, 1, []int64{10, 11})
	require.NoError(t, err)
	assert.Len(t, m, 1)

	// Setting user 2's marks does not touch user 1's.
	no := false
	_, err = issues_model.SetAscInboxState(ctx, 2, 10, &no, nil, now)
	require.NoError(t, err)
	mine, err := issues_model.GetAscInboxState(ctx, 1, 10)
	require.NoError(t, err)
	require.NotNil(t, mine)
	assert.True(t, mine.Pinned)

	require.NoError(t, issues_model.DeleteAscInboxStatesByPull(ctx, 10))
	mine, err = issues_model.GetAscInboxState(ctx, 1, 10)
	require.NoError(t, err)
	assert.Nil(t, mine)
}

func TestAscInboxStateReviewers(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	ctx := t.Context()
	const issue = int64(9001)
	rows := []*issues_model.Review{
		{IssueID: issue, ReviewerID: 2, Type: issues_model.ReviewTypeRequest},
		{IssueID: issue, ReviewerID: 2, Type: issues_model.ReviewTypeReject, CommitID: "h1", Official: true},
		{IssueID: issue, ReviewerID: 3, Type: issues_model.ReviewTypeApprove, CommitID: "h1"},
		{IssueID: issue, ReviewerID: 3, Type: issues_model.ReviewTypePending, CommitID: "h2"},
		{IssueID: issue, ReviewerID: 4, Type: issues_model.ReviewTypeReject, CommitID: "h1", Dismissed: true},
		{IssueID: issue, ReviewerTeamID: 5, Type: issues_model.ReviewTypeRequest},
	}
	for _, r := range rows {
		require.NoError(t, db.Insert(ctx, r))
	}
	got, err := issues_model.GetAscInboxReviewers(ctx, issue)
	require.NoError(t, err)
	assert.ElementsMatch(t, []issues_model.AscInboxReviewer{
		{UserID: 2, Type: issues_model.ReviewTypeReject, Official: true, CommitID: "h1"},
		{UserID: 3, Type: issues_model.ReviewTypeApprove, CommitID: "h1"},
	}, got)
}
