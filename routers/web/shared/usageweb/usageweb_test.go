// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package usageweb

import (
	gocontext "context"
	"html/template"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"forgejo.org/models/unittest"
	"forgejo.org/modules/ascclient"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/base"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/templates"
	"forgejo.org/modules/test"
	"forgejo.org/services/ascidentity"
	"forgejo.org/services/asctrust"
	"forgejo.org/services/context"
	"forgejo.org/services/contexttest"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

var forwardedKey = []byte("0123456789abcdef0123456789abcdef")

const internalToken = "internal-token-for-tests"

type fakeUsage struct {
	ascv1.UnimplementedRepoServer
	ascv1.UnimplementedUsageServer

	mu       sync.Mutex
	features []string
	md       []metadata.MD
	reqs     []any

	usage func(*ascv1.GetUsageRequest) (*ascv1.GetUsageResponse, error)
	spend func(*ascv1.ListSpendRequest) (*ascv1.ListSpendResponse, error)
	quota func(*ascv1.GetQuotaRequest) (*ascv1.GetQuotaResponse, error)
	set   func(*ascv1.SetQuotaRequest) (*ascv1.SetQuotaResponse, error)
}

func (f *fakeUsage) note(ctx gocontext.Context, req any) {
	md, _ := metadata.FromIncomingContext(ctx)
	f.mu.Lock()
	f.md, f.reqs = append(f.md, md), append(f.reqs, req)
	f.mu.Unlock()
}

func (f *fakeUsage) Version(gocontext.Context, *ascv1.VersionRequest) (*ascv1.VersionResponse, error) {
	return &ascv1.VersionResponse{Features: f.features}, nil
}

func (f *fakeUsage) GetUsage(ctx gocontext.Context, r *ascv1.GetUsageRequest) (*ascv1.GetUsageResponse, error) {
	f.note(ctx, r)
	if f.usage == nil {
		return f.UnimplementedUsageServer.GetUsage(ctx, r)
	}
	return f.usage(r)
}

func (f *fakeUsage) ListSpend(ctx gocontext.Context, r *ascv1.ListSpendRequest) (*ascv1.ListSpendResponse, error) {
	f.note(ctx, r)
	if f.spend == nil {
		return f.UnimplementedUsageServer.ListSpend(ctx, r)
	}
	return f.spend(r)
}

func (f *fakeUsage) GetQuota(ctx gocontext.Context, r *ascv1.GetQuotaRequest) (*ascv1.GetQuotaResponse, error) {
	f.note(ctx, r)
	if f.quota == nil {
		return f.UnimplementedUsageServer.GetQuota(ctx, r)
	}
	return f.quota(r)
}

func (f *fakeUsage) SetQuota(ctx gocontext.Context, r *ascv1.SetQuotaRequest) (*ascv1.SetQuotaResponse, error) {
	f.note(ctx, r)
	if f.set == nil {
		return f.UnimplementedUsageServer.SetQuota(ctx, r)
	}
	return f.set(r)
}

func (f *fakeUsage) calls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.reqs)
}

func start(t *testing.T, features ...string) *fakeUsage {
	t.Helper()
	require.NoError(t, unittest.PrepareTestDatabase())
	f := &fakeUsage{features: features}
	srv := grpc.NewServer()
	ascv1.RegisterRepoServer(srv, f)
	ascv1.RegisterUsageServer(srv, f)
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	go func() { _ = srv.Serve(l) }()
	t.Cleanup(srv.Stop)
	c, err := ascclient.New(ascclient.Config{Target: l.Addr().String(), InternalToken: internalToken, Retry: ascclient.RetryConfig{MaxAttempts: 1}})
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Close() })
	t.Cleanup(asctrust.UseConn(c))
	t.Cleanup(test.MockVariableValue(&setting.Asc.ForwardedKey, forwardedKey))
	t.Cleanup(test.MockVariableValue(&setting.Asc.Enabled, true))
	return f
}

func (f *fakeUsage) assertClaim(t *testing.T, i int, user, role, subject string) {
	t.Helper()
	f.mu.Lock()
	md := f.md[i]
	f.mu.Unlock()
	assert.Equal(t, []string{"Bearer " + internalToken}, md.Get("authorization"))
	got, gotRole, err := ascidentity.Verify(forwardedKey, func(k string) string { return strings.Join(md.Get(k), ",") }, subject, time.Now())
	require.NoError(t, err, "the claim is signed for %s", subject)
	assert.Equal(t, user, got)
	assert.Equal(t, role, gotRole)
}

type resp struct {
	status int
	body   string
	loc    string
	flash  string
}

func run(t *testing.T, method, target string, user int64, form url.Values, h func(*context.Context)) resp {
	t.Helper()
	ctx, rec := contexttest.MockContext(t, method+" "+target)
	ctx.Render = templates.HTMLRenderer()
	contexttest.LoadUser(t, ctx, user)
	ctx.IsSigned = true
	ctx.Data["CsrfTokenHtml"] = template.HTML(`<input type="hidden" name="_csrf" value="csrf-token-for-tests">`)
	if form != nil {
		ctx.Req.Form, ctx.Req.PostForm = form, form
	}
	h(ctx)
	r := resp{status: rec.Code, body: rec.Body.String(), loc: rec.Header().Get("Location")}
	if ctx.Flash != nil {
		r.flash = ctx.Flash.ErrorMsg + ctx.Flash.SuccessMsg
	}
	return r
}

func userLevel() *Level {
	return &Level{
		Owner: &ascv1.OwnerRef{Kind: ascv1.OwnerKind_OWNER_KIND_USER, Name: "user2"},
		Call:  asctrust.CallAccount, Base: "/user/settings/asc/usage", Label: "user2", Tpl: base.TplName("user/settings/asc_usage"),
	}
}

func adminLevel() *Level {
	return &Level{
		Owner: &ascv1.OwnerRef{Kind: ascv1.OwnerKind_OWNER_KIND_INSTANCE},
		Call:  asctrust.CallInstance, Base: "/admin/asc/usage", Label: "this server", Tpl: base.TplName("user/settings/asc_usage"), Admin: true,
	}
}

func usageAnswer() *ascv1.GetUsageResponse {
	return &ascv1.GetUsageResponse{
		Period: "2026-10", PeriodStart: 1790812800, ResetsAt: 1793491200,
		Storage:      &ascv1.UsageMeter{Used: 3 << 30, Limit: 4 << 30},
		AgentMinutes: &ascv1.UsageMeter{Used: 120, Limit: 100, Over: true},
		AiSpend:      &ascv1.UsageMeter{Used: 12_345_678},
		Bandwidth:    &ascv1.UsageMeter{Used: 5},
		OverQuota:    true, ByoAiSpendMicroUsd: 2_000_000,
		Repos: []*ascv1.UsageRepo{{Repo: "user2/<b>repo1</b>", StorageBytes: 1 << 20, AgentMinutes: 7, AiSpendMicroUsd: 1500}},
	}
}

func TestUserUsagePage(t *testing.T) {
	f := start(t, asctrust.FeatureUsage)
	f.usage = func(r *ascv1.GetUsageRequest) (*ascv1.GetUsageResponse, error) { return usageAnswer(), nil }
	f.spend = func(r *ascv1.ListSpendRequest) (*ascv1.ListSpendResponse, error) {
		return &ascv1.ListSpendResponse{
			Rows:  []*ascv1.SpendRow{{Group: "k_1", Label: "Work key‮", Requests: 3, InputTokens: 10, OutputTokens: 20, CostMicroUsd: 2_500_000}},
			Total: &ascv1.SpendRow{Requests: 3, CostMicroUsd: 2_500_000},
		}, nil
	}
	r := run(t, "GET", "/user/settings/asc/usage?period=2026-10&group=repo", 2, nil, func(c *context.Context) { Serve(c, userLevel()) })
	require.Equal(t, http.StatusOK, r.status, r.body)
	for _, want := range []string{`data-testid="asc-usage-storage"`, "3.0 GiB", "$12.35", `data-testid="asc-usage-over"`, `data-testid="asc-usage-byo"`, "Work key", "$2.50"} {
		assert.Contains(t, r.body, want)
	}
	assert.NotContains(t, r.body, "<b>repo1</b>", "a repository name from the server is escaped")
	assert.Contains(t, r.body, `data-over="true"`)
	assert.NotContains(t, r.body, `data-testid="asc-usage-quota"`, "a person cannot set limits")
	assert.NotContains(t, r.body, `data-testid="asc-usage-owner"`)

	req := f.reqs[0].(*ascv1.GetUsageRequest)
	assert.Equal(t, "2026-10", req.GetPeriod())
	assert.Equal(t, "user2", req.GetOwner().GetName())
	assert.Equal(t, ascv1.SpendGroup_SPEND_GROUP_REPO, f.reqs[1].(*ascv1.ListSpendRequest).GetGroupBy())
	assert.EqualValues(t, 1790812800, f.reqs[1].(*ascv1.ListSpendRequest).GetSince())
	f.assertClaim(t, 0, "2:user2", ascidentity.RoleWrite, "user:user2")
}

func TestUsagePageRefusesWhatItCannotTrust(t *testing.T) {
	f := start(t, asctrust.FeatureUsage)
	r := run(t, "GET", "/x?period=2026-13", 2, nil, func(c *context.Context) { Serve(c, userLevel()) })
	assert.Equal(t, http.StatusBadRequest, r.status, "a bad period is refused before a call")
	assert.Zero(t, f.calls())

	f.usage = func(*ascv1.GetUsageRequest) (*ascv1.GetUsageResponse, error) {
		a := usageAnswer()
		a.Period = "../etc"
		return a, nil
	}
	r = run(t, "GET", "/x", 2, nil, func(c *context.Context) { Serve(c, userLevel()) })
	assert.Equal(t, http.StatusBadGateway, r.status)
	assert.Contains(t, r.body, `data-testid="asc-usage-problem"`)
	assert.NotContains(t, r.body, "../etc")
}

func TestUsagePageWithoutTheFeature(t *testing.T) {
	f := start(t)
	r := run(t, "GET", "/x", 2, nil, func(c *context.Context) { Serve(c, userLevel()) })
	assert.Equal(t, http.StatusOK, r.status)
	assert.Contains(t, r.body, `data-testid="asc-usage-unsupported"`)
	assert.Zero(t, f.calls())
}

func TestAdminUsageNamesTheOwnerAndSetsLimits(t *testing.T) {
	f := start(t, asctrust.FeatureUsage)
	f.usage = func(r *ascv1.GetUsageRequest) (*ascv1.GetUsageResponse, error) { return usageAnswer(), nil }
	f.spend = func(r *ascv1.ListSpendRequest) (*ascv1.ListSpendResponse, error) {
		return &ascv1.ListSpendResponse{Total: &ascv1.SpendRow{}}, nil
	}
	f.quota = func(r *ascv1.GetQuotaRequest) (*ascv1.GetQuotaResponse, error) {
		return &ascv1.GetQuotaResponse{
			Override:        &ascv1.UsageQuota{StorageBytes: 5 << 30, AiSpendMicroUsd: 12_500_000},
			InstanceDefault: &ascv1.UsageQuota{StorageBytes: 2 << 30},
			Effective:       &ascv1.UsageQuota{StorageBytes: 5 << 30},
		}, nil
	}
	r := run(t, "GET", "/admin/asc/usage?kind=org&name=Org3", 1, nil, func(c *context.Context) { Serve(c, adminLevel()) })
	require.Equal(t, http.StatusOK, r.status, r.body)
	assert.Contains(t, r.body, `data-testid="asc-usage-quota"`)
	assert.Contains(t, r.body, `data-testid="asc-usage-owner"`)
	assert.Contains(t, r.body, `value="5"`, "the own limit fills the field")
	assert.Contains(t, r.body, `value="12.5"`)
	assert.Contains(t, r.body, `placeholder="2.0 GiB"`, "the default shows as the placeholder")
	req := f.reqs[0].(*ascv1.GetUsageRequest)
	assert.Equal(t, ascv1.OwnerKind_OWNER_KIND_ORG, req.GetOwner().GetKind())
	assert.Equal(t, "org3", req.GetOwner().GetName(), "the name is lower case")
	f.assertClaim(t, 0, "1:user1", ascidentity.RoleAdmin, "instance")

	n := f.calls()
	r = run(t, "GET", "/admin/asc/usage?kind=group&name=x", 1, nil, func(c *context.Context) { Serve(c, adminLevel()) })
	assert.Equal(t, http.StatusBadRequest, r.status)
	r = run(t, "GET", "/admin/asc/usage?kind=user&name=../x", 1, nil, func(c *context.Context) { Serve(c, adminLevel()) })
	assert.Equal(t, http.StatusBadRequest, r.status)
	assert.Equal(t, n, f.calls(), "nothing is sent for an owner of a bad shape")

	f.set = func(r *ascv1.SetQuotaRequest) (*ascv1.SetQuotaResponse, error) {
		return &ascv1.SetQuotaResponse{Quota: &ascv1.GetQuotaResponse{Effective: r.GetQuota(), Override: r.GetQuota(), InstanceDefault: &ascv1.UsageQuota{}}}, nil
	}
	form := url.Values{"kind": {"user"}, "name": {"Alice"}, "storage_gib": {"10"}, "agent_minutes": {"1,000"}, "ai_spend_usd": {"$25.50"}, "bandwidth_gib": {""}, "op": {uuid.Must(uuid.NewV7()).String()}}
	r = run(t, "POST", "/admin/asc/usage/quota", 1, form, func(c *context.Context) { ServeSetQuota(c, adminLevel()) })
	assert.Equal(t, http.StatusSeeOther, r.status)
	assert.Equal(t, "/admin/asc/usage?kind=user&name=alice", r.loc)
	set := f.reqs[f.calls()-1].(*ascv1.SetQuotaRequest)
	assert.Equal(t, "alice", set.GetOwner().GetName())
	assert.EqualValues(t, 10<<30, set.GetQuota().GetStorageBytes())
	assert.EqualValues(t, 1000, set.GetQuota().GetAgentMinutes())
	assert.EqualValues(t, 25_500_000, set.GetQuota().GetAiSpendMicroUsd())
	assert.Zero(t, set.GetQuota().GetBandwidthBytes())

	n = f.calls()
	for _, bad := range []url.Values{
		{"storage_gib": {"-1"}, "op": {uuid.Must(uuid.NewV7()).String()}},
		{"ai_spend_usd": {"1e9"}, "op": {uuid.Must(uuid.NewV7()).String()}},
		{"agent_minutes": {"99999999999"}, "op": {uuid.Must(uuid.NewV7()).String()}},
	} {
		r = run(t, "POST", "/admin/asc/usage/quota", 1, bad, func(c *context.Context) { ServeSetQuota(c, adminLevel()) })
		assert.Equal(t, http.StatusSeeOther, r.status)
		assert.NotEmpty(t, r.flash)
	}
	assert.Equal(t, n, f.calls(), "a bad limit is refused before a call")

	// A level that is not the administrator's has no way to set a limit.
	r = run(t, "POST", "/user/settings/asc/usage/quota", 2, form, func(c *context.Context) { ServeSetQuota(c, userLevel()) })
	assert.Equal(t, http.StatusForbidden, r.status)
	assert.Equal(t, n, f.calls())
}
