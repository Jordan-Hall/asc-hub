// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package moderation_test

import (
	"testing"

	"forgejo.org/models/moderation"
	"forgejo.org/models/unittest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// suspend and deleteRepo are the content actions of services/moderation, which this package cannot import.
const (
	suspend    = 1
	deleteRepo = 3
)

func record(t *testing.T, d *moderation.Decision) *moderation.Decision {
	t.Helper()
	require.NoError(t, moderation.RecordDecision(t.Context(), d))
	require.Positive(t, d.ID)
	return d
}

func TestDecisionsAreTheOwnersAndTheNewestSuspensionIsFound(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	ctx := t.Context()
	has, err := moderation.HasDecisions(ctx, 4)
	require.NoError(t, err)
	assert.False(t, has)

	first := record(t, &moderation.Decision{ContentType: moderation.ReportedContentTypeUser, ContentID: 4, ContentRef: "@user4", OwnerID: 4, Action: suspend, Reason: "spam", ModeratorID: 1})
	repo := record(t, &moderation.Decision{ContentType: moderation.ReportedContentTypeRepository, ContentID: 7, ContentRef: "user4/x", OwnerID: 4, Action: deleteRepo, ModeratorID: 1})
	second := record(t, &moderation.Decision{ContentType: moderation.ReportedContentTypeUser, ContentID: 4, ContentRef: "@user4", OwnerID: 4, Action: suspend, Reason: "again", ModeratorID: 1, AppealStatus: moderation.AppealOverturned})
	record(t, &moderation.Decision{ContentType: moderation.ReportedContentTypeUser, ContentID: 5, OwnerID: 5, Action: suspend, ModeratorID: 1})

	assert.Equal(t, moderation.AppealNone, second.AppealStatus, "a new decision is never already appealed")
	has, err = moderation.HasDecisions(ctx, 4)
	require.NoError(t, err)
	assert.True(t, has)

	list, err := moderation.DecisionsOf(ctx, 4, 50)
	require.NoError(t, err)
	require.Len(t, list, 3)
	for _, d := range list {
		assert.EqualValues(t, 4, d.OwnerID)
	}
	list, err = moderation.DecisionsOf(ctx, 4, 1)
	require.NoError(t, err)
	assert.Len(t, list, 1)

	latest, err := moderation.LatestSuspension(ctx, 4, suspend)
	require.NoError(t, err)
	require.NotNil(t, latest)
	assert.Equal(t, second.ID, latest.ID, "the newest suspension, not the repository decision")
	assert.NotEqual(t, first.ID, latest.ID)
	assert.NotEqual(t, repo.ID, latest.ID)

	none, err := moderation.LatestSuspension(ctx, 2, suspend)
	require.NoError(t, err)
	assert.Nil(t, none)
	missing, err := moderation.GetDecision(ctx, 99999)
	require.NoError(t, err)
	assert.Nil(t, missing)
}

func TestAnAppealIsTheOwnersOnceAndIsAnsweredOnce(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	ctx := t.Context()
	d := record(t, &moderation.Decision{ContentType: moderation.ReportedContentTypeRepository, ContentID: 7, ContentRef: "user4/x", OwnerID: 4, Action: deleteRepo, ModeratorID: 1})

	require.ErrorIs(t, moderation.Appeal(ctx, d.ID, 5, "not mine"), moderation.ErrAppealNotAllowed, "only the owner appeals")
	require.ErrorIs(t, moderation.Appeal(ctx, d.ID, 4, ""), moderation.ErrAppealNotAllowed, "an appeal says something")
	long := make([]rune, moderation.MaxAppealText+1)
	for i := range long {
		long[i] = 'a'
	}
	require.ErrorIs(t, moderation.Appeal(ctx, d.ID, 4, string(long)), moderation.ErrAppealNotAllowed)
	require.ErrorIs(t, moderation.DecideAppeal(ctx, d.ID, 1, true, "no appeal yet"), moderation.ErrAppealNotAllowed)

	require.NoError(t, moderation.Appeal(ctx, d.ID, 4, "It was a fork of my own project."))
	require.ErrorIs(t, moderation.Appeal(ctx, d.ID, 4, "again"), moderation.ErrAppealNotAllowed, "once")

	open, err := moderation.OpenAppeals(ctx, 50)
	require.NoError(t, err)
	require.Len(t, open, 1)
	assert.Equal(t, d.ID, open[0].ID)
	assert.True(t, open[0].IsAppealOpen())
	assert.False(t, open[0].CanAppeal())
	assert.Equal(t, "It was a fork of my own project.", open[0].AppealText)
	n, err := moderation.CountOpenAppeals(ctx)
	require.NoError(t, err)
	assert.EqualValues(t, 1, n)

	require.NoError(t, moderation.DecideAppeal(ctx, d.ID, 1, false, "The link was phishing."))
	require.ErrorIs(t, moderation.DecideAppeal(ctx, d.ID, 1, true, "changed my mind"), moderation.ErrAppealNotAllowed, "answered once")
	got, err := moderation.GetDecision(ctx, d.ID)
	require.NoError(t, err)
	assert.Equal(t, moderation.AppealUpheld, got.AppealStatus)
	assert.Equal(t, "The link was phishing.", got.AppealReason)
	assert.EqualValues(t, 1, got.AppealDeciderID)
	assert.NotZero(t, got.AppealDecidedUnix)
	n, err = moderation.CountOpenAppeals(ctx)
	require.NoError(t, err)
	assert.Zero(t, n)
}
