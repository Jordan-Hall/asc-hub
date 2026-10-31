// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package admin

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"forgejo.org/models/moderation"
	"forgejo.org/modules/templates"
	"forgejo.org/services/asctrust"
	"forgejo.org/services/contexttest"
	moderation_service "forgejo.org/services/moderation"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestModerationReasonIsCleanedAndBounded(t *testing.T) {
	assert.Equal(t, "Spam links\nin the profile", moderationReason("  Spam links\x00\x1b\nin the profile  "))
	assert.Empty(t, moderationReason(" \t\x00 "))
	assert.Len(t, []rune(moderationReason(strings.Repeat("é", 900))), maxModerationReason)
}

func TestModerationDetailsNameWhatWasReportedAndDone(t *testing.T) {
	target, d := moderationDetails(moderation.ReportedContentTypeRepository, 12, moderation_service.ContentActionDeleteRepo, moderation_service.ReportActionMarkAsHandled, "Malware")
	assert.Equal(t, "repository:12", target)
	assert.Equal(t, map[string]string{"content_type": "repository", "content_id": "12", "content_action": "delete_repository", "report_action": "mark_as_handled", "reason": "Malware"}, d)
	_, d = moderationDetails(moderation.ReportedContentTypeUser, 3, moderation_service.ContentActionNone, moderation_service.ReportActionMarkAsIgnored, "")
	assert.NotContains(t, d, "reason", "no reason, none recorded")
}

func TestPerformActionNeedsAReasonToRemoveAndRecordsWhatItDid(t *testing.T) {
	f := startInstanceAgents(t, asctrust.FeatureAuditAppend)
	act := func(form url.Values) (int, string) {
		ctx, rec := contexttest.MockContext(t, "POST /admin/abuse_reports/act")
		ctx.Render = templates.HTMLRenderer()
		contexttest.LoadUser(t, ctx, 1)
		ctx.IsSigned = true
		ctx.Req.Form, ctx.Req.PostForm = form, form
		PerformAction(ctx)
		return rec.Code, rec.Body.String()
	}
	base := func(extra url.Values) url.Values {
		v := url.Values{"content_type": {"1"}, "content_id": {"2"}}
		for k, vals := range extra {
			v[k] = vals
		}
		return v
	}

	// removing an account without a reason is refused, and nothing is recorded
	code, body := act(base(url.Values{"content_action": {"1"}, "report_action": {"1"}}))
	require.Equal(t, http.StatusOK, code, body)
	assert.Empty(t, f.appends)

	// dismissing needs no reason and is recorded without one
	code, _ = act(base(url.Values{"report_action": {"2"}}))
	require.Equal(t, http.StatusOK, code)
	require.Len(t, f.appends, 1)
	assert.Equal(t, asctrust.AuditHostModerationActed, f.appends[0].GetAction())
	assert.Equal(t, "user1", f.appends[0].GetActor())
	assert.Equal(t, "user:2", f.appends[0].GetTarget())
	assert.Equal(t, "mark_as_ignored", f.appends[0].GetDetails()["report_action"])
	assert.NotContains(t, f.appends[0].GetDetails(), "reason")

	// a reason is kept with the action
	code, _ = act(base(url.Values{"report_action": {"1"}, "reason": {"Checked: not abuse"}}))
	require.Equal(t, http.StatusOK, code)
	require.Len(t, f.appends, 2)
	assert.Equal(t, "Checked: not abuse", f.appends[1].GetDetails()["reason"])
}
