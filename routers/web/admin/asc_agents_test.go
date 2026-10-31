// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package admin

import (
	gocontext "context"
	"html/template"
	"net"
	"net/http"
	"net/url"
	"sync"
	"testing"

	"forgejo.org/models/unittest"
	"forgejo.org/modules/ascclient"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/templates"
	"forgejo.org/modules/test"
	"forgejo.org/services/asctrust"
	"forgejo.org/services/contexttest"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
)

type fakeInstanceAgents struct {
	ascv1.UnimplementedRepoServer
	ascv1.UnimplementedOrgSettingsServer
	ascv1.UnimplementedAuditServer

	mu        sync.Mutex
	features  []string
	sets      []*ascv1.SetInstanceAgentsRequest
	appends   []*ascv1.AppendAuditRequest
	appendErr error
	events    []*ascv1.AuditEvent
	listed    []*ascv1.ListAuditRequest
}

func (f *fakeInstanceAgents) List(_ gocontext.Context, r *ascv1.ListAuditRequest) (*ascv1.ListAuditResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.listed = append(f.listed, r)
	return &ascv1.ListAuditResponse{Events: f.events}, nil
}

func (f *fakeInstanceAgents) Append(_ gocontext.Context, r *ascv1.AppendAuditRequest) (*ascv1.AppendAuditResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.appendErr != nil {
		return nil, f.appendErr
	}
	f.appends = append(f.appends, r)
	return &ascv1.AppendAuditResponse{Seq: uint64(len(f.appends))}, nil
}

func (f *fakeInstanceAgents) Version(gocontext.Context, *ascv1.VersionRequest) (*ascv1.VersionResponse, error) {
	return &ascv1.VersionResponse{Features: f.features}, nil
}

func (f *fakeInstanceAgents) GetInstanceAgents(gocontext.Context, *ascv1.GetInstanceAgentsRequest) (*ascv1.GetInstanceAgentsResponse, error) {
	return &ascv1.GetInstanceAgentsResponse{AllowAgents: true, DefaultBudgetMicroUsd: 400_000_000}, nil
}

func (f *fakeInstanceAgents) SetInstanceAgents(_ gocontext.Context, r *ascv1.SetInstanceAgentsRequest) (*ascv1.SetInstanceAgentsResponse, error) {
	f.mu.Lock()
	f.sets = append(f.sets, r)
	f.mu.Unlock()
	return &ascv1.SetInstanceAgentsResponse{Settings: &ascv1.GetInstanceAgentsResponse{AllowAgents: r.GetAllowAgents(), DefaultBudgetMicroUsd: r.GetDefaultBudgetMicroUsd()}}, nil
}

func startInstanceAgents(t *testing.T, features ...string) *fakeInstanceAgents {
	t.Helper()
	require.NoError(t, unittest.PrepareTestDatabase())
	f := &fakeInstanceAgents{features: features}
	srv := grpc.NewServer()
	ascv1.RegisterRepoServer(srv, f)
	ascv1.RegisterOrgSettingsServer(srv, f)
	ascv1.RegisterAuditServer(srv, f)
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	go func() { _ = srv.Serve(l) }()
	t.Cleanup(srv.Stop)
	c, err := ascclient.New(ascclient.Config{Target: l.Addr().String(), InternalToken: "internal-token-for-tests", Retry: ascclient.RetryConfig{MaxAttempts: 1}})
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Close() })
	t.Cleanup(asctrust.UseConn(c))
	t.Cleanup(test.MockVariableValue(&setting.Asc.ForwardedKey, []byte("0123456789abcdef0123456789abcdef")))
	t.Cleanup(test.MockVariableValue(&setting.Asc.Enabled, true))
	return f
}

func TestAscInstanceAgents(t *testing.T) {
	f := startInstanceAgents(t, asctrust.FeatureOrgSettings)

	ctx, rec := contexttest.MockContext(t, "GET /admin/asc/agents")
	ctx.Render = templates.HTMLRenderer()
	contexttest.LoadUser(t, ctx, 1)
	ctx.IsSigned = true
	AscAgents(ctx)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), `data-testid="asc-agents-form"`)
	assert.Contains(t, rec.Body.String(), `value="400"`)
	assert.Contains(t, rec.Body.String(), `name="allow_agents" value="1" checked`)

	post := func(user int64, form url.Values) (int, string) {
		ctx, rec := contexttest.MockContext(t, "POST /admin/asc/agents")
		ctx.Render = templates.HTMLRenderer()
		contexttest.LoadUser(t, ctx, user)
		ctx.IsSigned = true
		ctx.Data["CsrfTokenHtml"] = template.HTML("")
		ctx.Req.Form, ctx.Req.PostForm = form, form
		AscAgentsSave(ctx)
		flash := ""
		if ctx.Flash != nil {
			flash = ctx.Flash.ErrorMsg
		}
		return rec.Code, flash
	}
	op := uuid.Must(uuid.NewV7()).String()
	code, _ := post(1, url.Values{"op": {op}, "budget_usd": {"250.5"}})
	assert.Equal(t, http.StatusSeeOther, code)
	require.Len(t, f.sets, 1)
	assert.False(t, f.sets[0].GetAllowAgents(), "an unchecked switch is off")
	assert.EqualValues(t, 250_500_000, f.sets[0].GetDefaultBudgetMicroUsd())

	code, flash := post(1, url.Values{"op": {op}, "budget_usd": {"-5"}})
	assert.Equal(t, http.StatusSeeOther, code)
	assert.NotEmpty(t, flash)
	code, _ = post(2, url.Values{"op": {op}, "budget_usd": {"5"}})
	assert.Equal(t, http.StatusForbidden, code, "only a site administrator")
	assert.Len(t, f.sets, 1)
}
