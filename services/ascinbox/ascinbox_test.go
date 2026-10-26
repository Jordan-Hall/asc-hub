// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package ascinbox

import (
	"testing"
	"time"

	"forgejo.org/models/db"
	issues_model "forgejo.org/models/issues"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/timeutil"
	"forgejo.org/services/authz"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func pullIDs(es []*Entry) []int64 {
	ids := make([]int64, 0, len(es))
	for _, e := range es {
		ids = append(ids, e.PullID)
	}
	return ids
}

func TestArrange(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	t0 := now.Add(-time.Hour)
	mk := func() []*Entry {
		return []*Entry{
			{PullID: 1, Section: issues_model.AscInboxWaiting, Updated: t0},
			{PullID: 2, Section: issues_model.AscInboxNeedsReview, Updated: t0},
			{PullID: 3, Section: issues_model.AscInboxNeedsReview, Updated: t0.Add(time.Minute)},
			{PullID: 4, Section: issues_model.AscInboxNeedsReview, Updated: t0.Add(2 * time.Minute)},
			{PullID: 5, Section: issues_model.AscInboxMerged, Updated: t0},
			{PullID: 6, Section: issues_model.AscInboxNeedsReview, Updated: t0.Add(time.Minute)},
			{PullID: 7, Section: issues_model.AscInboxNone, Updated: t0},
			{PullID: 8, Section: "made_up", Updated: t0},
		}
	}
	states := map[int64]*issues_model.AscInboxState{
		2: {Pinned: true},
		4: {SnoozedUntil: timeutil.TimeStamp(now.Add(time.Hour).Unix())},
		5: {SnoozedUntil: timeutil.TimeStamp(now.Add(-time.Hour).Unix())}, // expired
	}

	t.Run("order, pins, snooze, unknown sections", func(t *testing.T) {
		got := Arrange(mk(), states, now, Options{})
		// needs_review: 2 pinned, then 6 and 3 (same time, higher pull id first); 4 is snoozed. Then waiting, merged.
		assert.Equal(t, []int64{2, 6, 3, 1, 5}, pullIDs(got))
		assert.True(t, got[0].Pinned)
		assert.True(t, got[4].SnoozedUntil.IsZero(), "an expired snooze shows no snooze time")
	})

	t.Run("snoozed entries on request carry the time", func(t *testing.T) {
		got := Arrange(mk(), states, now, Options{IncludeSnoozed: true})
		assert.Equal(t, []int64{2, 4, 6, 3, 1, 5}, pullIDs(got))
		assert.Equal(t, now.Add(time.Hour).Unix(), got[1].SnoozedUntil.Unix())
	})

	t.Run("section filter", func(t *testing.T) {
		got := Arrange(mk(), states, now, Options{Sections: []issues_model.AscInboxSection{issues_model.AscInboxMerged, issues_model.AscInboxWaiting}})
		assert.Equal(t, []int64{1, 5}, pullIDs(got))
		assert.Empty(t, Arrange(mk(), nil, now, Options{Sections: []issues_model.AscInboxSection{issues_model.AscInboxDrafts}}))
	})

	t.Run("stable across calls and input order", func(t *testing.T) {
		in := mk()
		a := pullIDs(Arrange(in, states, now, Options{}))
		for i, j := 0, len(in)-1; i < j; i, j = i+1, j-1 {
			in[i], in[j] = in[j], in[i]
		}
		assert.Equal(t, a, pullIDs(Arrange(in, states, now, Options{})))
	})

	t.Run("marks of an earlier call do not stick", func(t *testing.T) {
		in := mk()
		Arrange(in, states, now, Options{IncludeSnoozed: true})
		got := Arrange(in, nil, now, Options{})
		for _, e := range got {
			assert.False(t, e.Pinned)
			assert.True(t, e.SnoozedUntil.IsZero())
		}
	})
}

func TestLoadAndList(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	ctx := t.Context()
	const reviewerID = 4
	reviewer := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: reviewerID})
	author := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 1})
	issue := unittest.AssertExistsAndLoadBean(t, &issues_model.Issue{ID: 3})
	require.True(t, issue.IsPull)
	require.NoError(t, issue.LoadRepo(ctx))
	require.Equal(t, int64(1), issue.PosterID)
	repoID := issue.RepoID
	require.NoError(t, db.Insert(ctx, &issues_model.Review{
		Type: issues_model.ReviewTypeRequest, IssueID: issue.ID, ReviewerID: reviewerID,
	}))
	all := &authz.AllAccessAuthorizationReducer{}
	public := &authz.PublicReposAuthorizationReducer{}
	setPrivate := func(p bool) {
		_, err := db.GetEngine(db.DefaultContext).ID(repoID).Cols("is_private").Update(&repo_model.Repository{IsPrivate: p})
		require.NoError(t, err)
	}
	t.Cleanup(func() { setPrivate(false) })
	find := func(u *user_model.User, r authz.AuthorizationReducer) *Entry {
		es, err := Load(ctx, u, r, time.Now())
		require.NoError(t, err)
		for _, e := range es {
			if e.PullID == 2 {
				return e
			}
		}
		return nil
	}

	setPrivate(false)
	e := find(reviewer, all)
	require.NotNil(t, e, "a requested reviewer sees the pull request")
	assert.Equal(t, issues_model.AscInboxNeedsReview, e.Section)
	assert.Equal(t, "issue3", e.Title)
	assert.Equal(t, int64(3), e.Index)
	assert.Contains(t, e.HTMLURL, "/pulls/3")
	assert.NotNil(t, find(author, all), "the author sees it")
	assert.NotNil(t, find(reviewer, public), "a public repository is within a public-only token")

	stranger := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 5})
	assert.Nil(t, find(stranger, all), "an uninvolved user does not see it")

	setPrivate(true)
	assert.Nil(t, find(reviewer, all), "a reviewer who cannot read the repository does not see its pull request")
	assert.NotNil(t, find(author, all), "a site administrator author still reads it")
	assert.Nil(t, find(author, public), "a token limited to public repositories hides a private one, even from an administrator")

	setPrivate(false)
	_, err := Load(ctx, reviewer, nil, time.Now())
	require.ErrorIs(t, err, ErrNoReducer)
	es, err := Load(ctx, nil, all, time.Now())
	require.NoError(t, err)
	assert.Empty(t, es)

	t.Run("List applies the marks of the doer only", func(t *testing.T) {
		now := time.Now()
		pinned := true
		_, err := issues_model.SetAscInboxState(ctx, author.ID, 2, &pinned, nil, now)
		require.NoError(t, err)
		snooze := now.Add(time.Hour)
		_, err = issues_model.SetAscInboxState(ctx, reviewerID, 2, nil, &snooze, now)
		require.NoError(t, err)

		got, err := List(ctx, author, all, now, Options{})
		require.NoError(t, err)
		require.Contains(t, pullIDs(got), int64(2))
		for _, e := range got {
			if e.PullID == 2 {
				assert.True(t, e.Pinned)
				assert.True(t, e.SnoozedUntil.IsZero(), "the reviewer's snooze is not the author's")
			}
		}

		got, err = List(ctx, reviewer, all, now, Options{})
		require.NoError(t, err)
		assert.NotContains(t, pullIDs(got), int64(2), "snoozed")
		got, err = List(ctx, reviewer, all, now, Options{IncludeSnoozed: true})
		require.NoError(t, err)
		require.Contains(t, pullIDs(got), int64(2))
		for _, e := range got {
			if e.PullID == 2 {
				assert.False(t, e.Pinned, "the author's pin is not the reviewer's")
				assert.Equal(t, snooze.Unix(), e.SnoozedUntil.Unix())
			}
		}
		got, err = List(ctx, reviewer, all, now, Options{IncludeSnoozed: true, Sections: []issues_model.AscInboxSection{issues_model.AscInboxMerged}})
		require.NoError(t, err)
		assert.NotContains(t, pullIDs(got), int64(2))
	})
}
