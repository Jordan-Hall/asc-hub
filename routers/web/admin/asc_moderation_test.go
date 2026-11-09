// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package admin

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"forgejo.org/models/db"
	"forgejo.org/models/moderation"
	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"
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

func TestAnswerAppealNeedsAReasonAndOverturningASuspensionLetsTheAccountIn(t *testing.T) {
	f := startInstanceAgents(t, asctrust.FeatureAuditAppend)
	ctx0 := t.Context()
	_, err := db.GetEngine(ctx0).Exec("UPDATE `user` SET prohibit_login = ? WHERE id = 4", true)
	require.NoError(t, err)
	d := &moderation.Decision{ContentType: moderation.ReportedContentTypeUser, ContentID: 4, ContentRef: "@user4", OwnerID: 4, Action: int(moderation_service.ContentActionSuspendAccount), Reason: "Spam.", ModeratorID: 1}
	require.NoError(t, moderation.RecordDecision(ctx0, d))
	require.NoError(t, moderation.Appeal(ctx0, d.ID, 4, "Not spam."))

	answer := func(form url.Values) (int, string) {
		ctx, rec := contexttest.MockContext(t, "POST /admin/moderation/appeals/1")
		ctx.Render = templates.HTMLRenderer()
		contexttest.LoadUser(t, ctx, 1)
		ctx.IsSigned = true
		ctx.SetParams(":id", strconv.FormatInt(d.ID, 10))
		ctx.Req.Form, ctx.Req.PostForm = form, form
		AnswerAppeal(ctx)
		return rec.Code, rec.Body.String()
	}

	code, _ := answer(url.Values{"answer": {"sideways"}, "reason": {"x"}})
	assert.Equal(t, http.StatusBadRequest, code)
	code, body := answer(url.Values{"answer": {"overturn"}})
	require.Equal(t, http.StatusOK, code, body)
	still, err := moderation.GetDecision(ctx0, d.ID)
	require.NoError(t, err)
	assert.True(t, still.IsAppealOpen(), "no reason, no answer")
	assert.Empty(t, f.appends)

	code, body = answer(url.Values{"answer": {"overturn"}, "reason": {"The links were the person's own docs."}})
	require.Equal(t, http.StatusOK, code, body)
	got, err := moderation.GetDecision(ctx0, d.ID)
	require.NoError(t, err)
	assert.Equal(t, moderation.AppealOverturned, got.AppealStatus)
	u := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 4})
	assert.False(t, u.ProhibitLogin, "the suspension is lifted")
	require.Len(t, f.appends, 1)
	assert.Equal(t, "overturned", f.appends[0].GetDetails()["appeal"])
	assert.Equal(t, "user:4", f.appends[0].GetTarget())

	// answered once: a second answer changes nothing
	code, _ = answer(url.Values{"answer": {"uphold"}, "reason": {"no"}})
	require.Equal(t, http.StatusOK, code)
	got, err = moderation.GetDecision(ctx0, d.ID)
	require.NoError(t, err)
	assert.Equal(t, moderation.AppealOverturned, got.AppealStatus)
	assert.Len(t, f.appends, 1)
}

func TestTheQueueListsOpenAppealsWithTheirOwner(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	d := &moderation.Decision{ContentType: moderation.ReportedContentTypeRepository, ContentID: 1, ContentRef: "user2/repo1", OwnerID: 2, Action: int(moderation_service.ContentActionDeleteRepo), Reason: "Phishing.", ModeratorID: 1}
	require.NoError(t, moderation.RecordDecision(t.Context(), d))
	require.NoError(t, moderation.Appeal(t.Context(), d.ID, 2, "It was a demo."))
	ctx, _ := contexttest.MockContext(t, "GET /admin/moderation/reports")
	require.True(t, loadAppeals(ctx))
	appeals := ctx.Data["Appeals"].([]moderationAppeal)
	require.Len(t, appeals, 1)
	assert.Equal(t, "user2", appeals[0].OwnerName)
	assert.Equal(t, "asc_admin.mod_action_delete_repo", appeals[0].ActionKey)
}

func TestOverturningAnOlderSuspensionLeavesTheNewerInForce(t *testing.T) {
	startInstanceAgents(t, asctrust.FeatureAuditAppend)
	ctx0 := t.Context()
	_, err := db.GetEngine(ctx0).Exec("UPDATE `user` SET prohibit_login = ? WHERE id = 4", true)
	require.NoError(t, err)
	suspend := int(moderation_service.ContentActionSuspendAccount)
	older := &moderation.Decision{ContentType: moderation.ReportedContentTypeUser, ContentID: 4, OwnerID: 4, Action: suspend, Reason: "first", ModeratorID: 1}
	require.NoError(t, moderation.RecordDecision(ctx0, older))
	require.NoError(t, moderation.Appeal(ctx0, older.ID, 4, "the first was wrong"))
	newer := &moderation.Decision{ContentType: moderation.ReportedContentTypeUser, ContentID: 4, OwnerID: 4, Action: suspend, Reason: "second", ModeratorID: 1}
	require.NoError(t, moderation.RecordDecision(ctx0, newer))

	ctx, rec := contexttest.MockContext(t, "POST /admin/moderation/appeals/1")
	ctx.Render = templates.HTMLRenderer()
	contexttest.LoadUser(t, ctx, 1)
	ctx.IsSigned = true
	ctx.SetParams(":id", strconv.FormatInt(older.ID, 10))
	form := url.Values{"answer": {"overturn"}, "reason": {"The first report was wrong."}}
	ctx.Req.Form, ctx.Req.PostForm = form, form
	AnswerAppeal(ctx)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	got, err := moderation.GetDecision(ctx0, older.ID)
	require.NoError(t, err)
	assert.Equal(t, moderation.AppealOverturned, got.AppealStatus)
	u := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 4})
	assert.True(t, u.ProhibitLogin, "the newer suspension stays")
}
