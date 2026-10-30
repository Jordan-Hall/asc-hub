// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package byoweb

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
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/ascclient"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/templates"
	"forgejo.org/modules/test"
	svc "forgejo.org/services/ascbyo"
	"forgejo.org/services/ascbyo/ascbyotest"
	"forgejo.org/services/ascidentity"
	"forgejo.org/services/asctrust"
	"forgejo.org/services/context"
	"forgejo.org/services/contexttest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

var sharesKey = []byte("0123456789abcdef0123456789abcdef")

const goodShareID = "as_abcdefghij0123456789"

type fakeShares struct {
	ascv1.UnimplementedRepoServer
	ascv1.UnimplementedAiKeySharesServer

	mu       sync.Mutex
	features []string
	md       []metadata.MD
	reqs     []any
}

func (f *fakeShares) note(ctx gocontext.Context, r any) {
	md, _ := metadata.FromIncomingContext(ctx)
	f.mu.Lock()
	f.md, f.reqs = append(f.md, md), append(f.reqs, r)
	f.mu.Unlock()
}

func (f *fakeShares) calls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.reqs)
}

func (f *fakeShares) Version(gocontext.Context, *ascv1.VersionRequest) (*ascv1.VersionResponse, error) {
	return &ascv1.VersionResponse{Features: f.features}, nil
}

func (f *fakeShares) ListShares(ctx gocontext.Context, r *ascv1.ListAiKeySharesRequest) (*ascv1.ListAiKeySharesResponse, error) {
	f.note(ctx, r)
	return &ascv1.ListAiKeySharesResponse{Shares: []*ascv1.AiKeyShare{{
		Id: goodShareID, KeyId: r.GetKeyId(), Grantee: &ascv1.OwnerRef{Kind: ascv1.OwnerKind_OWNER_KIND_ORG, Name: "acme<i>"}, AllowUse: true, SeeSpend: true, Created: 1790812800,
	}}}, nil
}

func (f *fakeShares) ShareUsage(ctx gocontext.Context, r *ascv1.AiKeyShareUsageRequest) (*ascv1.AiKeyShareUsageResponse, error) {
	f.note(ctx, r)
	return &ascv1.AiKeyShareUsageResponse{Rows: []*ascv1.AiKeyShareUsageRow{{ShareId: goodShareID, Requests: 4, CostMicroUsd: 1_250_000}, {Requests: 9, CostMicroUsd: 3_000_000}}}, nil
}

func (f *fakeShares) GetKeyCaps(ctx gocontext.Context, r *ascv1.GetAiKeyCapsRequest) (*ascv1.GetAiKeyCapsResponse, error) {
	f.note(ctx, r)
	return &ascv1.GetAiKeyCapsResponse{Caps: &ascv1.AiKeyCaps{WarnPercent: 80, StopAtCap: true, MonthlyCapMicroUsd: 25_000_000, MonthSpentMicroUsd: 4_250_000}}, nil
}

func (f *fakeShares) ShareKey(ctx gocontext.Context, r *ascv1.ShareAiKeyRequest) (*ascv1.ShareAiKeyResponse, error) {
	f.note(ctx, r)
	return &ascv1.ShareAiKeyResponse{Share: &ascv1.AiKeyShare{Id: goodShareID, Grantee: r.GetGrantee(), AllowUse: r.GetAllowUse(), SeeSpend: r.GetSeeSpend()}}, nil
}

func (f *fakeShares) RevokeShare(ctx gocontext.Context, r *ascv1.RevokeAiKeyShareRequest) (*ascv1.RevokeAiKeyShareResponse, error) {
	f.note(ctx, r)
	return &ascv1.RevokeAiKeyShareResponse{}, nil
}

func (f *fakeShares) SetKeyCaps(ctx gocontext.Context, r *ascv1.SetAiKeyCapsRequest) (*ascv1.SetAiKeyCapsResponse, error) {
	f.note(ctx, r)
	return &ascv1.SetAiKeyCapsResponse{Caps: &ascv1.AiKeyCaps{WarnPercent: r.GetWarnPercent(), StopAtCap: r.GetStopAtCap()}}, nil
}

func (f *fakeShares) ListSharedWithMe(ctx gocontext.Context, r *ascv1.ListSharedAiKeysRequest) (*ascv1.ListSharedAiKeysResponse, error) {
	f.note(ctx, r)
	return &ascv1.ListSharedAiKeysResponse{Keys: []*ascv1.SharedAiKey{{
		ShareId: goodShareID, KeyId: "ak_x", Label: "Team key", Provider: ascv1.AiProvider_AI_PROVIDER_ANTHROPIC,
		Owner: &ascv1.OwnerRef{Kind: ascv1.OwnerKind_OWNER_KIND_ORG, Name: "acme"}, Usable: true, AllowUse: true, SeeSpend: true, MonthSpentMicroUsd: 500_000,
	}}}, nil
}

func startShares(t *testing.T, features ...string) *fakeShares {
	t.Helper()
	f := &fakeShares{features: features}
	srv := grpc.NewServer()
	ascv1.RegisterRepoServer(srv, f)
	ascv1.RegisterAiKeySharesServer(srv, f)
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	go func() { _ = srv.Serve(l) }()
	t.Cleanup(srv.Stop)
	c, err := ascclient.New(ascclient.Config{Target: l.Addr().String(), InternalToken: "internal-token-for-tests", Retry: ascclient.RetryConfig{MaxAttempts: 1}})
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Close() })
	t.Cleanup(asctrust.UseConn(c))
	t.Cleanup(test.MockVariableValue(&setting.Asc.ForwardedKey, sharesKey))
	return f
}

func (f *fakeShares) assertClaim(t *testing.T, i int) {
	t.Helper()
	f.mu.Lock()
	md := f.md[i]
	f.mu.Unlock()
	assert.Equal(t, []string{"Bearer internal-token-for-tests"}, md.Get("authorization"))
	user, role, err := ascidentity.Verify(sharesKey, func(k string) string { return strings.Join(md.Get(k), ",") }, "user:alice", time.Now())
	require.NoError(t, err, "the claim is signed for the account of the person")
	assert.Equal(t, "2:alice", user)
	assert.Equal(t, ascidentity.RoleWrite, role)
}

// runWith is run with more path parameters.
func runWith(t *testing.T, fn func(*context.Context, *Level), lvl *Level, form url.Values, params map[string]string) result {
	t.Helper()
	ctx, resp := contexttest.MockContext(t, "POST "+lvl.Base+"/x")
	ctx.Locale = locale(t)
	ctx.TemplateContext = context.NewTemplateContextForWeb(ctx)
	ctx.Render = templates.HTMLRenderer()
	ctx.Doer = &user_model.User{ID: 2, Name: "alice", LowerName: "alice", IsActive: true}
	ctx.IsSigned = true
	ctx.Data["CsrfTokenHtml"] = template.HTML(`<input type="hidden" name="_csrf" value="csrf-token">`)
	ctx.Req.PostForm, ctx.Req.Form = form, form
	for k, v := range params {
		ctx.SetParams(k, v)
	}
	fn(ctx, lvl)
	return result{ctx, resp}
}

func TestAIKeySharing(t *testing.T) {
	f := setup(t)
	sh := startShares(t, asctrust.FeatureAiKeyShares)
	lvl := userLevel(t)

	r := run(t, AICreate, lvl, http.MethodPost, url.Values{"label": {"Team"}, "provider": {"anthropic"}, "api_key": {"sk-ant-api03-SUPERSECRETKEYVALUE9876"}, "op_id": {ascbyotest.OpIDForTest}}, "")
	require.Equal(t, http.StatusSeeOther, r.resp.Code, r.body())
	id := strings.TrimPrefix(r.loc(), "/user/settings/asc/ai/keys/")
	require.NotEmpty(t, id)
	_ = f

	t.Run("the key page shows the shares, the use and the cap rules", func(t *testing.T) {
		r := run(t, AIKey, lvl, http.MethodGet, nil, id)
		require.Equal(t, http.StatusOK, r.resp.Code, r.body())
		body := r.body()
		for _, want := range []string{`data-testid="asc-key-caps"`, `data-testid="asc-key-shares"`, `data-testid="asc-key-share-form"`, "$1.25", "$3.00", `value="80"`, "$4.25"} {
			assert.Contains(t, body, want)
		}
		assert.NotContains(t, body, "acme<i>", "a name from the server is escaped")
		assert.Contains(t, body, `name="stop_at_cap" value="on" checked`)
		assert.Contains(t, body, "/keys/"+id+"/shares/"+goodShareID+"/revoke")
		assert.NotContains(t, body, "SUPERSECRET")
		sh.assertClaim(t, 0)
	})

	t.Run("the list shows keys shared with the person", func(t *testing.T) {
		r := run(t, AIIndex, lvl, http.MethodGet, nil, "")
		require.Equal(t, http.StatusOK, r.resp.Code, r.body())
		assert.Contains(t, r.body(), `data-testid="asc-shared-with-me"`)
		assert.Contains(t, r.body(), "Team key")
		assert.Contains(t, r.body(), "$0.50")
	})

	t.Run("share, revoke and caps", func(t *testing.T) {
		n := sh.calls()
		r := run(t, AIShare, lvl, http.MethodPost, url.Values{"kind": {"org"}, "name": {"Acme"}, "allow_use": {"on"}, "op_id": {ascbyotest.OpIDForTest}}, id)
		assert.Equal(t, http.StatusSeeOther, r.resp.Code)
		assert.Equal(t, "/user/settings/asc/ai/keys/"+id, r.loc())
		req := sh.reqs[n].(*ascv1.ShareAiKeyRequest)
		assert.Equal(t, id, req.GetKeyId())
		assert.Equal(t, "acme", req.GetGrantee().GetName())
		assert.True(t, req.GetAllowUse())
		assert.False(t, req.GetSeeSpend(), "an unchecked box is off")

		r = runWith(t, AIShareRevoke, lvl, url.Values{"op_id": {ascbyotest.OpIDForTest}}, map[string]string{"id": id, "share": goodShareID})
		assert.Equal(t, http.StatusSeeOther, r.resp.Code)
		assert.Equal(t, goodShareID, sh.reqs[n+1].(*ascv1.RevokeAiKeyShareRequest).GetShareId())

		r = run(t, AICaps, lvl, http.MethodPost, url.Values{"warn_percent": {"90"}, "stop_at_cap": {"on"}, "op_id": {ascbyotest.OpIDForTest}}, id)
		assert.Equal(t, http.StatusSeeOther, r.resp.Code)
		caps := sh.reqs[n+2].(*ascv1.SetAiKeyCapsRequest)
		assert.EqualValues(t, 90, caps.GetWarnPercent())
		assert.True(t, caps.GetStopAtCap())
	})

	t.Run("a mistake of the form is refused before a call", func(t *testing.T) {
		n := sh.calls()
		for _, bad := range []url.Values{
			{"kind": {"team"}, "name": {"acme"}},
			{"kind": {"repo"}, "name": {"not-a-repo"}},
			{"kind": {"org"}, "name": {"a/b"}},
			{"kind": {"user"}, "name": {""}},
		} {
			bad.Set("op_id", ascbyotest.OpIDForTest)
			r := run(t, AIShare, lvl, http.MethodPost, bad, id)
			assert.Equal(t, http.StatusSeeOther, r.resp.Code)
			assert.NotEmpty(t, r.ctx.Flash.ErrorMsg, "%v", bad)
		}
		r := run(t, AICaps, lvl, http.MethodPost, url.Values{"warn_percent": {"101"}, "op_id": {ascbyotest.OpIDForTest}}, id)
		assert.NotEmpty(t, r.ctx.Flash.ErrorMsg)
		r = runWith(t, AIShareRevoke, lvl, url.Values{"op_id": {ascbyotest.OpIDForTest}}, map[string]string{"id": id, "share": "../x"})
		assert.NotEmpty(t, r.ctx.Flash.ErrorMsg)
		assert.Equal(t, n, sh.calls())
	})

	t.Run("a repository has no sharing", func(t *testing.T) {
		n := sh.calls()
		r := run(t, AIShare, repoLevel(t, false), http.MethodPost, url.Values{"kind": {"org"}, "name": {"acme"}, "op_id": {ascbyotest.OpIDForTest}}, id)
		assert.Equal(t, http.StatusNotFound, r.resp.Code)
		assert.Equal(t, n, sh.calls())
	})
}

func TestAIKeySharingNeedsTheService(t *testing.T) {
	setup(t)
	sh := startShares(t)
	lvl := userLevel(t)
	r := run(t, AICreate, lvl, http.MethodPost, url.Values{"label": {"Team"}, "provider": {"anthropic"}, "api_key": {"sk-ant-api03-SUPERSECRETKEYVALUE9876"}, "op_id": {ascbyotest.OpIDForTest}}, "")
	id := strings.TrimPrefix(r.loc(), "/user/settings/asc/ai/keys/")
	r = run(t, AIKey, lvl, http.MethodGet, nil, id)
	require.Equal(t, http.StatusOK, r.resp.Code)
	assert.NotContains(t, r.body(), `data-testid="asc-key-shares"`)
	assert.Zero(t, sh.calls(), "a server without the service is not asked")
}

func TestAIKeySharingOfAnOrganization(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	setup(t)
	sh := startShares(t, asctrust.FeatureAiKeyShares)
	lvl := &Level{Scope: mustScope(t, svc.KindOrg, "org3"), Base: "/org/org3/settings/asc", ScopeLabel: "org3", StorageTpl: pageStorage, AITpl: pageAI}
	r := run(t, AICreate, lvl, http.MethodPost, url.Values{"label": {"Team"}, "provider": {"anthropic"}, "api_key": {"sk-ant-api03-SUPERSECRETKEYVALUE9876"}, "op_id": {ascbyotest.OpIDForTest}}, "")
	require.Equal(t, http.StatusSeeOther, r.resp.Code, r.body())
	id := strings.TrimPrefix(r.loc(), "/org/org3/settings/asc/ai/keys/")

	r = run(t, AIKey, lvl, http.MethodGet, nil, id)
	require.Equal(t, http.StatusOK, r.resp.Code)
	assert.Contains(t, r.body(), `data-testid="asc-key-shares"`)
	sh.mu.Lock()
	md := sh.md[0]
	sh.mu.Unlock()
	user, role, err := ascidentity.Verify(sharesKey, func(k string) string { return strings.Join(md.Get(k), ",") }, "org:org3", time.Now())
	require.NoError(t, err, "the claim is signed for the organization")
	assert.Equal(t, "2:alice", user)
	assert.Equal(t, ascidentity.RoleAdmin, role, "an owner of the organization")
}
