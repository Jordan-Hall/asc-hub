// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	gocontext "context"
	"html/template"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	auth_model "forgejo.org/models/auth"
	perm_model "forgejo.org/models/perm"
	"forgejo.org/models/unit"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/ascclient"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/templates"
	"forgejo.org/modules/test"
	"forgejo.org/services/asctrust"
	"forgejo.org/services/context"
	"forgejo.org/services/contexttest"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

// A fake asc serve for the trust pages that read the operation log, the CI insights and the stack service. A test
// sets the hook of each method it expects; a method without a hook answers Unimplemented. Every call is recorded with
// the credential, the op id and the request.

type trustCall struct {
	Method string
	Auth   string
	OpID   string
	MD     metadata.MD
	Req    any
}

type fakeTrust struct {
	ascv1.UnimplementedRepoServer
	ascv1.UnimplementedStacksServer
	ascv1.UnimplementedCiInsightsServer
	ascv1.UnimplementedAuditServer
	ascv1.UnimplementedDepsServiceServer

	mu       sync.Mutex
	calls    []trustCall
	features []string

	listRefLog func(*ascv1.ListRefLogRequest) (*ascv1.ListRefLogResponse, error)
	undoRef    func(*ascv1.UndoRefRequest) (*ascv1.UndoRefResponse, error)
	listGroups func(*ascv1.ListFailureGroupsRequest) (*ascv1.ListFailureGroupsResponse, error)
	listFlaky  func(*ascv1.ListFlakyTestsRequest) (*ascv1.ListFlakyTestsResponse, error)
	history    func(*ascv1.FlakyHistoryRequest) (*ascv1.FlakyHistoryResponse, error)
	auditList  func(*ascv1.ListAuditRequest) (*ascv1.ListAuditResponse, error)
	auditChain func(*ascv1.VerifyAuditChainRequest) (*ascv1.VerifyAuditChainResponse, error)

	depFindings func(*ascv1.ListDepFindingsRequest) (*ascv1.ListDepFindingsResponse, error)
	depUpdates  func(*ascv1.ListDepUpdatesRequest) (*ascv1.ListDepUpdatesResponse, error)
	depConfig   func(*ascv1.GetDepsConfigRequest) (*ascv1.GetDepsConfigResponse, error)
	depScan     func(*ascv1.ScanDepsRequest) (*ascv1.ScanDepsResponse, error)
	depPropose  func(*ascv1.ProposeDepUpdateRequest) (*ascv1.ProposeDepUpdateResponse, error)
	depDismiss  func(*ascv1.DismissDepFindingRequest) (*ascv1.DismissDepFindingResponse, error)
}

func (f *fakeTrust) note(ctx gocontext.Context, method string, req any) {
	md, _ := metadata.FromIncomingContext(ctx)
	c := trustCall{Method: method, Req: req, MD: md}
	if v := md.Get("authorization"); len(v) > 0 {
		c.Auth = v[0]
	}
	if v := md.Get("x-asc-op-id"); len(v) > 0 {
		c.OpID = v[0]
	}
	f.mu.Lock()
	f.calls = append(f.calls, c)
	f.mu.Unlock()
}

func (f *fakeTrust) callsOf(method string) []trustCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []trustCall
	for _, c := range f.calls {
		if c.Method == method {
			out = append(out, c)
		}
	}
	return out
}

func (f *fakeTrust) Version(ctx gocontext.Context, _ *ascv1.VersionRequest) (*ascv1.VersionResponse, error) {
	f.note(ctx, "Version", nil)
	return &ascv1.VersionResponse{Features: f.features}, nil
}

func (f *fakeTrust) ListRefLog(ctx gocontext.Context, r *ascv1.ListRefLogRequest) (*ascv1.ListRefLogResponse, error) {
	f.note(ctx, "ListRefLog", r)
	if f.listRefLog == nil {
		return f.UnimplementedStacksServer.ListRefLog(ctx, r)
	}
	return f.listRefLog(r)
}

func (f *fakeTrust) UndoRef(ctx gocontext.Context, r *ascv1.UndoRefRequest) (*ascv1.UndoRefResponse, error) {
	f.note(ctx, "UndoRef", r)
	if f.undoRef == nil {
		return f.UnimplementedStacksServer.UndoRef(ctx, r)
	}
	return f.undoRef(r)
}

func (f *fakeTrust) ListFailureGroups(ctx gocontext.Context, r *ascv1.ListFailureGroupsRequest) (*ascv1.ListFailureGroupsResponse, error) {
	f.note(ctx, "ListFailureGroups", r)
	if f.listGroups == nil {
		return f.UnimplementedCiInsightsServer.ListFailureGroups(ctx, r)
	}
	return f.listGroups(r)
}

func (f *fakeTrust) ListFlakyTests(ctx gocontext.Context, r *ascv1.ListFlakyTestsRequest) (*ascv1.ListFlakyTestsResponse, error) {
	f.note(ctx, "ListFlakyTests", r)
	if f.listFlaky == nil {
		return f.UnimplementedCiInsightsServer.ListFlakyTests(ctx, r)
	}
	return f.listFlaky(r)
}

func (f *fakeTrust) FlakyHistory(ctx gocontext.Context, r *ascv1.FlakyHistoryRequest) (*ascv1.FlakyHistoryResponse, error) {
	f.note(ctx, "FlakyHistory", r)
	if f.history == nil {
		return f.UnimplementedCiInsightsServer.FlakyHistory(ctx, r)
	}
	return f.history(r)
}

func (f *fakeTrust) List(ctx gocontext.Context, r *ascv1.ListAuditRequest) (*ascv1.ListAuditResponse, error) {
	f.note(ctx, "AuditList", r)
	if f.auditList == nil {
		return f.UnimplementedAuditServer.List(ctx, r)
	}
	return f.auditList(r)
}

func (f *fakeTrust) ListActions(ctx gocontext.Context, r *ascv1.ListAuditActionsRequest) (*ascv1.ListAuditActionsResponse, error) {
	f.note(ctx, "AuditActions", r)
	return &ascv1.ListAuditActionsResponse{}, nil
}

func (f *fakeTrust) VerifyChain(ctx gocontext.Context, r *ascv1.VerifyAuditChainRequest) (*ascv1.VerifyAuditChainResponse, error) {
	f.note(ctx, "VerifyChain", r)
	if f.auditChain == nil {
		return f.UnimplementedAuditServer.VerifyChain(ctx, r)
	}
	return f.auditChain(r)
}

func (f *fakeTrust) ListFindings(ctx gocontext.Context, r *ascv1.ListDepFindingsRequest) (*ascv1.ListDepFindingsResponse, error) {
	f.note(ctx, "DepFindings", r)
	if f.depFindings == nil {
		return f.UnimplementedDepsServiceServer.ListFindings(ctx, r)
	}
	return f.depFindings(r)
}

func (f *fakeTrust) ListUpdates(ctx gocontext.Context, r *ascv1.ListDepUpdatesRequest) (*ascv1.ListDepUpdatesResponse, error) {
	f.note(ctx, "DepUpdates", r)
	if f.depUpdates == nil {
		return f.UnimplementedDepsServiceServer.ListUpdates(ctx, r)
	}
	return f.depUpdates(r)
}

func (f *fakeTrust) GetConfig(ctx gocontext.Context, r *ascv1.GetDepsConfigRequest) (*ascv1.GetDepsConfigResponse, error) {
	f.note(ctx, "DepConfig", r)
	if f.depConfig == nil {
		return f.UnimplementedDepsServiceServer.GetConfig(ctx, r)
	}
	return f.depConfig(r)
}

func (f *fakeTrust) Scan(ctx gocontext.Context, r *ascv1.ScanDepsRequest) (*ascv1.ScanDepsResponse, error) {
	f.note(ctx, "DepScan", r)
	if f.depScan == nil {
		return f.UnimplementedDepsServiceServer.Scan(ctx, r)
	}
	return f.depScan(r)
}

func (f *fakeTrust) ProposeUpdate(ctx gocontext.Context, r *ascv1.ProposeDepUpdateRequest) (*ascv1.ProposeDepUpdateResponse, error) {
	f.note(ctx, "DepPropose", r)
	if f.depPropose == nil {
		return f.UnimplementedDepsServiceServer.ProposeUpdate(ctx, r)
	}
	return f.depPropose(r)
}

func (f *fakeTrust) Dismiss(ctx gocontext.Context, r *ascv1.DismissDepFindingRequest) (*ascv1.DismissDepFindingResponse, error) {
	f.note(ctx, "DepDismiss", r)
	if f.depDismiss == nil {
		return f.UnimplementedDepsServiceServer.Dismiss(ctx, r)
	}
	return f.depDismiss(r)
}

// startFakeTrust serves the fake on a loopback port, points asctrust at it, makes the repository 1 an asc
// repository and switches [asc] on. The call credential is a fixed token, so no row is written to the database.
func startFakeTrust(t *testing.T, features ...string) *fakeTrust {
	t.Helper()
	f := &fakeTrust{features: features}
	srv := grpc.NewServer()
	ascv1.RegisterRepoServer(srv, f)
	ascv1.RegisterStacksServer(srv, f)
	ascv1.RegisterCiInsightsServer(srv, f)
	ascv1.RegisterAuditServer(srv, f)
	ascv1.RegisterDepsServiceServer(srv, f)
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	go func() { _ = srv.Serve(l) }()
	t.Cleanup(srv.Stop)
	c, err := ascclient.New(ascclient.Config{Target: l.Addr().String(), InternalToken: "internal-token-for-tests", Retry: ascclient.RetryConfig{MaxAttempts: 1}})
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Close() })
	t.Cleanup(asctrust.UseConn(c))
	t.Cleanup(asctrust.UseTokenIssuer(func(gocontext.Context, *user_model.User, auth_model.AccessTokenScope, int64) (string, func(), error) {
		return "per-request-token", func() {}, nil
	}))
	t.Cleanup(test.MockVariableValue(&setting.Asc.Enabled, true))
	setStorageBackend(t, 1, "asc")
	return f
}

// trustPage is the answer of a handler run by runTrust.
type trustPage struct {
	status int
	body   string
	loc    string
	ctx    *context.Context
	flash  string
}

type trustReq struct {
	method string
	path   string
	// user is the signed-in user (0 is nobody); access is the person's access to the code and the pull requests.
	user   int64
	access perm_model.AccessMode
	// noActions takes away the right to read the Actions of the repository, noCode the right to read the code.
	noActions bool
	noCode    bool
	form      url.Values
	params    map[string]string
}

// runTrust runs a handler of the trust pages for repository 1 (user2/repo1).
func runTrust(t *testing.T, r trustReq, handler func(*context.Context)) trustPage {
	t.Helper()
	if r.method == "" {
		r.method = http.MethodGet
	}
	ctx, resp := contexttest.MockContext(t, r.method+" "+r.path)
	ctx.Render = templates.HTMLRenderer()
	if r.user != 0 {
		contexttest.LoadUser(t, ctx, r.user)
		ctx.IsSigned = true
	}
	contexttest.LoadRepo(t, ctx, 1)
	ctx.Data["RepoLink"] = ctx.Repo.RepoLink
	ctx.Data["CsrfTokenHtml"] = template.HTML(`<input type="hidden" name="_csrf" value="csrf-token-for-tests">`)
	ctx.Repo.Permission.AccessMode = r.access
	ctx.Repo.Permission.UnitsMode = map[unit.Type]perm_model.AccessMode{
		unit.TypeCode: r.access, unit.TypePullRequests: r.access, unit.TypeActions: r.access,
	}
	if r.noCode {
		ctx.Repo.Permission.UnitsMode[unit.TypeCode] = perm_model.AccessModeNone
	}
	if r.noActions {
		ctx.Repo.Permission.UnitsMode[unit.TypeActions] = perm_model.AccessModeNone
	}
	if r.form != nil {
		if r.method == http.MethodPost {
			ctx.Req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			ctx.Req.Body = io.NopCloser(strings.NewReader(r.form.Encode()))
		}
		ctx.Req.Form = r.form
	}
	for k, v := range r.params {
		ctx.SetParams(k, v)
	}
	handler(ctx)
	return trustPage{status: resp.Code, body: resp.Body.String(), loc: resp.Header().Get("Location"), ctx: ctx}
}

var _ = httptest.NewRecorder
