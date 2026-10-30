// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package admin

import (
	"net/http"
	"net/url"
	"testing"

	"forgejo.org/modules/setting"
	"forgejo.org/modules/templates"
	"forgejo.org/services/asctrust"
	"forgejo.org/services/contexttest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func changeHubSetting(t *testing.T, key, value string) (int, string) {
	t.Helper()
	ctx, rec := contexttest.MockContext(t, "POST /admin/config")
	ctx.Render = templates.HTMLRenderer()
	contexttest.LoadUser(t, ctx, 1)
	ctx.IsSigned = true
	ctx.Req.RemoteAddr = "203.0.113.9:4455"
	ctx.Req.Header.Set("User-Agent", "TestBrowser/1")
	form := url.Values{"key": {key}, "value": {value}}
	ctx.Req.Form, ctx.Req.PostForm = form, form
	ChangeConfig(ctx)
	return rec.Code, rec.Body.String()
}

func TestHubSettingChangesAreAppendedToTheAuditLogOfAsc(t *testing.T) {
	f := startInstanceAgents(t, asctrust.FeatureAuditAppend)
	regKey := setting.Config().Service.AllowRegistration.DynKey()

	code, body := changeHubSetting(t, regKey, "false")
	require.Equal(t, http.StatusOK, code, body)
	require.Len(t, f.appends, 1)
	got := f.appends[0]
	assert.Equal(t, asctrust.AuditHostSettingUpdated, got.GetAction())
	assert.Equal(t, "user1", got.GetActor(), "the person who changed it, as the hub verified")
	assert.Equal(t, regKey, got.GetTarget())
	assert.Equal(t, regKey, got.GetDetails()["setting"])
	assert.Equal(t, "false", got.GetDetails()["new"])
	assert.Contains(t, got.GetDetails(), "old")
	assert.Equal(t, "203.0.113.9", got.GetRemoteAddr(), "the address without the port")
	assert.Equal(t, "TestBrowser/1", got.GetUserAgent())

	// A second change appends a second event.
	code, _ = changeHubSetting(t, regKey, "true")
	require.Equal(t, http.StatusOK, code)
	require.Len(t, f.appends, 2)
	assert.Equal(t, "true", f.appends[1].GetDetails()["new"])
	assert.Equal(t, "false", f.appends[1].GetDetails()["old"], "the value before the change")

	// A change that is refused is not audited as a change.
	code, _ = changeHubSetting(t, "no.such.setting", "1")
	assert.NotEqual(t, http.StatusOK, code)
	assert.Len(t, f.appends, 2)
}

func TestAuditAppendNeverBlocksTheChangeAndNeedsTheFeature(t *testing.T) {
	regKey := setting.Config().Service.AllowRegistration.DynKey()

	// An asc without Audit.Append is not asked; the change stands.
	f := startInstanceAgents(t, asctrust.FeatureOrgSettings)
	code, body := changeHubSetting(t, regKey, "false")
	assert.Equal(t, http.StatusOK, code, body)
	assert.Empty(t, f.appends)

	// An asc that refuses the append does not undo or fail the change.
	f = startInstanceAgents(t, asctrust.FeatureAuditAppend)
	f.appendErr = status.Error(codes.Internal, "audit database is down")
	code, body = changeHubSetting(t, regKey, "true")
	assert.Equal(t, http.StatusOK, code, body)
	assert.Empty(t, f.appends)

	// Without asc nothing is sent.
	f = startInstanceAgents(t, asctrust.FeatureAuditAppend)
	asctrust.RecordHostAudit(t.Context(), asctrust.HostAudit{Action: asctrust.AuditHostSettingUpdated, Actor: ""})
	assert.Empty(t, f.appends, "no actor: nothing to record")
}

func TestMaintenanceChangesAreAppendedToTheAuditLogOfAsc(t *testing.T) {
	f := startInstanceAgents(t, asctrust.FeatureAuditAppend)
	ctx, rec := contexttest.MockContext(t, "POST /admin/maintenance")
	ctx.Render = templates.HTMLRenderer()
	contexttest.LoadUser(t, ctx, 1)
	ctx.IsSigned = true
	form := url.Values{"on": {"1"}, "message": {"Back at noon"}, "until": {"2026-11-01T12:00"}}
	ctx.Req.Form, ctx.Req.PostForm = form, form
	MaintenancePost(ctx)
	require.Equal(t, http.StatusSeeOther, rec.Code)
	require.Len(t, f.appends, 1)
	got := f.appends[0]
	assert.Equal(t, asctrust.AuditHostMaintenanceUpdated, got.GetAction())
	assert.Equal(t, "user1", got.GetActor())
	assert.Equal(t, "true", got.GetDetails()["enabled"])
	assert.Equal(t, "Back at noon", got.GetDetails()["message"])
	assert.Equal(t, "2026-11-01T12:00:00Z", got.GetDetails()["until"])
}
