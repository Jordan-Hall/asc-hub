// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package moderation_test

import (
	"testing"

	"forgejo.org/models/db"
	"forgejo.org/models/moderation"
	"forgejo.org/models/unittest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func agentReport(token string, contentID int64) *moderation.AbuseReport {
	return &moderation.AbuseReport{
		ReporterAgent: "reviewer", ReporterAgentToken: token,
		ContentType: moderation.ReportedContentTypeRepository, ContentID: contentID,
		Category: moderation.AbuseCategoryTypeSpam, Remarks: "spam links",
	}
}

func TestAnAgentTokenHasOneOpenReportPerContent(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	first := agentReport("at_a", 1)
	existing, err := moderation.ReportAbuseByAgent(t.Context(), first)
	require.NoError(t, err)
	assert.Nil(t, existing)
	assert.True(t, first.IsAgentReport())

	existing, err = moderation.ReportAbuseByAgent(t.Context(), agentReport("at_a", 1))
	require.NoError(t, err)
	require.NotNil(t, existing)
	assert.Equal(t, first.ID, existing.ID)

	other, err := moderation.ReportAbuseByAgent(t.Context(), agentReport("at_b", 1))
	require.NoError(t, err)
	assert.Nil(t, other, "another token reports the same content on its own")

	_, err = moderation.ReportAbuseByAgent(t.Context(), &moderation.AbuseReport{ContentType: moderation.ReportedContentTypeRepository, ContentID: 1})
	require.Error(t, err, "an agent report names its agent and token")
}

func TestAnAgentTokenHasABoundedNumberOfOpenReports(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	for i := range moderation.MaxOpenAgentReports {
		_, err := moderation.ReportAbuseByAgent(t.Context(), agentReport("at_flood", int64(1000+i)))
		require.NoError(t, err)
	}
	_, err := moderation.ReportAbuseByAgent(t.Context(), agentReport("at_flood", 5000))
	require.ErrorIs(t, err, moderation.ErrAgentReportLimit)
}

func TestEveryAgentReportTakesTheLockOfItsToken(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	for i := range 3 {
		_, err := moderation.ReportAbuseByAgent(t.Context(), agentReport("at_lock", int64(1+i%2)))
		require.NoError(t, err)
	}
	lock := &moderation.AgentReportLock{Token: "at_lock"}
	has, err := db.GetEngine(t.Context()).Get(lock)
	require.NoError(t, err)
	require.True(t, has)
	assert.EqualValues(t, 3, lock.Seq, "a duplicate takes the lock too")
}
