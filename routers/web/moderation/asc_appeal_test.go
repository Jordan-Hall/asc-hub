// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package moderation

import (
	"net/http"
	"net/url"
	"strconv"
	"testing"

	"forgejo.org/models/db"
	"forgejo.org/models/moderation"
	"forgejo.org/models/unittest"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/templates"
	"forgejo.org/modules/test"
	"forgejo.org/services/context"
	"forgejo.org/services/contexttest"
	moderation_service "forgejo.org/services/moderation"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func decisionOf(t *testing.T, owner int64, action moderation_service.ContentAction, ct moderation.ReportedContentType) *moderation.Decision {
	t.Helper()
	d := &moderation.Decision{ContentType: ct, ContentID: owner, ContentRef: "ref", OwnerID: owner, Action: int(action), Reason: "Because.", ModeratorID: 1}
	require.NoError(t, moderation.RecordDecision(t.Context(), d))
	return d
}

func page(t *testing.T, method string, uid int64, id int64, form url.Values, h func(*context.Context)) (int, string, string) {
	t.Helper()
	ctx, rec := contexttest.MockContext(t, method+" /-/moderation/decisions/"+strconv.FormatInt(id, 10))
	ctx.Render = templates.HTMLRenderer()
	contexttest.LoadUser(t, ctx, uid)
	ctx.IsSigned = true
	ctx.SetParams(":id", strconv.FormatInt(id, 10))
	if form != nil {
		ctx.Req.Form, ctx.Req.PostForm = form, form
	}
	h(ctx)
	return rec.Code, rec.Body.String(), rec.Header().Get("Location")
}

func TestOnlyTheOwnerSeesAndAppealsADecision(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	d := decisionOf(t, 2, moderation_service.ContentActionDeleteRepo, moderation.ReportedContentTypeRepository)

	code, body, _ := page(t, "GET", 2, d.ID, nil, Decision)
	require.Equal(t, http.StatusOK, code)
	assert.Contains(t, body, "Because.")
	assert.Contains(t, body, `name="text"`, "the owner may appeal")

	code, _, _ = page(t, "GET", 5, d.ID, nil, Decision)
	assert.Equal(t, http.StatusNotFound, code, "another person's decision does not exist for them")
	code, _, _ = page(t, "POST", 5, d.ID, url.Values{"text": {"mine"}}, AppealPost)
	assert.Equal(t, http.StatusNotFound, code)

	code, _, loc := page(t, "POST", 2, d.ID, url.Values{"text": {"  \x00 "}}, AppealPost)
	assert.Equal(t, http.StatusSeeOther, code)
	assert.Contains(t, loc, "/-/moderation/decisions/")
	got, err := moderation.GetDecision(t.Context(), d.ID)
	require.NoError(t, err)
	assert.True(t, got.CanAppeal(), "an empty appeal is not one")

	code, _, _ = page(t, "POST", 2, d.ID, url.Values{"text": {"It was a demo page."}}, AppealPost)
	assert.Equal(t, http.StatusSeeOther, code)
	got, err = moderation.GetDecision(t.Context(), d.ID)
	require.NoError(t, err)
	assert.True(t, got.IsAppealOpen())
	assert.Equal(t, "It was a demo page.", got.AppealText)

	code, body, _ = page(t, "GET", 2, d.ID, nil, Decision)
	require.Equal(t, http.StatusOK, code)
	assert.NotContains(t, body, `name="text"`, "once")
	assert.Contains(t, body, "It was a demo page.")

	code, body, _ = page(t, "GET", 2, 0, nil, Decisions)
	require.Equal(t, http.StatusOK, code)
	assert.Contains(t, body, d.ContentRef)
}

func TestTheSignInPageOffersTheSuspendedAccountItsAppeal(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	t.Cleanup(test.MockVariableValue(&setting.Moderation.Enabled, true))
	_, err := db.GetEngine(t.Context()).Exec("UPDATE `user` SET prohibit_login = ? WHERE id = 4", true)
	require.NoError(t, err)

	ctx, _ := contexttest.MockContext(t, "POST /user/login")
	SuspendedAppeal(ctx, 4)
	assert.Nil(t, ctx.Data["AscSuspension"], "no suspension recorded, nothing to appeal")

	d := decisionOf(t, 4, moderation_service.ContentActionSuspendAccount, moderation.ReportedContentTypeUser)
	ctx, _ = contexttest.MockContext(t, "POST /user/login")
	SuspendedAppeal(ctx, 4)
	require.NotNil(t, ctx.Data["AscSuspension"])
	appealCode, _ := ctx.Data["AscAppealCode"].(string)
	require.NotEmpty(t, appealCode)

	// an account that may sign in gets nothing
	ctx, _ = contexttest.MockContext(t, "POST /user/login")
	SuspendedAppeal(ctx, 2)
	assert.Nil(t, ctx.Data["AscSuspension"])

	send := func(form url.Values) string {
		ctx, rec := contexttest.MockContext(t, "POST /-/moderation/appeal")
		ctx.Req.Form, ctx.Req.PostForm = form, form
		AppealSignInPost(ctx)
		require.Equal(t, http.StatusSeeOther, rec.Code, rec.Body.String())
		return rec.Header().Get("Location")
	}
	id := strconv.FormatInt(d.ID, 10)
	assert.Contains(t, send(url.Values{"decision": {id}, "code": {"forged:code"}, "text": {"hi"}}), "/user/login")
	got, err := moderation.GetDecision(t.Context(), d.ID)
	require.NoError(t, err)
	assert.True(t, got.CanAppeal(), "a forged code records nothing")

	send(url.Values{"decision": {id}, "code": {appealCode}, "text": {"I am not a spammer."}})
	got, err = moderation.GetDecision(t.Context(), d.ID)
	require.NoError(t, err)
	assert.True(t, got.IsAppealOpen())

	// the page now shows the state and no form
	ctx, _ = contexttest.MockContext(t, "POST /user/login")
	SuspendedAppeal(ctx, 4)
	require.NotNil(t, ctx.Data["AscSuspension"])
	assert.Nil(t, ctx.Data["AscAppealCode"])
}
