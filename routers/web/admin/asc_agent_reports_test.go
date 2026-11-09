// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package admin

import (
	"net/http"
	"strconv"
	"testing"

	"forgejo.org/models/moderation"
	"forgejo.org/models/unittest"
	"forgejo.org/modules/templates"
	"forgejo.org/services/contexttest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTheQueueShowsWhichReportsAgentsSent(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	require.NoError(t, moderation.ReportAbuse(t.Context(), &moderation.AbuseReport{
		ReporterID: 2, ContentType: moderation.ReportedContentTypeRepository, ContentID: 1,
		Category: moderation.AbuseCategoryTypeSpam, Remarks: "a person saw it",
	}))
	_, err := moderation.ReportAbuseByAgent(t.Context(), &moderation.AbuseReport{
		ReporterAgent: "triage", ReporterAgentToken: "at_queue", ContentType: moderation.ReportedContentTypeRepository, ContentID: 1,
		Category: moderation.AbuseCategoryTypeSpam, Remarks: "spam links",
	})
	require.NoError(t, err)

	ctx, rec := contexttest.MockContext(t, "GET /admin/moderation/reports")
	ctx.Render = templates.HTMLRenderer()
	contexttest.LoadUser(t, ctx, 1)
	ctx.IsSigned = true
	AbuseReports(ctx)
	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	assert.Contains(t, body, "asc_admin.mod_reported_people_agents", "the count splits people and agents")

	ctx, rec = contexttest.MockContext(t, "GET /admin/moderation/reports/type/2/id/1")
	ctx.Render = templates.HTMLRenderer()
	contexttest.LoadUser(t, ctx, 1)
	ctx.IsSigned = true
	ctx.SetParams(":type", strconv.Itoa(int(moderation.ReportedContentTypeRepository)))
	ctx.SetParams(":id", "1")
	AbuseReportDetails(ctx)
	require.Equal(t, http.StatusOK, rec.Code)
	body = rec.Body.String()
	assert.Contains(t, body, "asc_admin.mod_report_by_agent", "the agent report names its agent")
}
