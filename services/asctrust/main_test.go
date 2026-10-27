// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package asctrust

import (
	"context"
	"net"
	"sync"
	"testing"

	"forgejo.org/models/unittest"
	"forgejo.org/modules/ascclient"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	_ "forgejo.org/modules/testimport"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

func TestMain(m *testing.M) {
	unittest.MainTest(m)
}

const testInternalToken = "internal-token-for-tests"

// fakeAsc is an `asc serve` for the trust services. Every service is a hook; a nil hook answers Unimplemented. It
// keeps the credential and the op id of every call, so a test can say whose rights a call used.
type fakeAsc struct {
	ascv1.UnimplementedRepoServer
	ascv1.UnimplementedPolicyServer
	ascv1.UnimplementedAuditServer
	ascv1.UnimplementedSecretScanServer
	ascv1.UnimplementedSigningServer
	ascv1.UnimplementedCiInsightsServer

	mu    sync.Mutex
	calls []fakeCall

	version     func() *ascv1.VersionResponse
	gitMap      func(*ascv1.GitMapRequest) (*ascv1.GitMapResponse, error)
	getSource   func(*ascv1.GetPolicySourceRequest) (*ascv1.GetPolicySourceResponse, error)
	validate    func(*ascv1.ValidatePolicyRequest) (*ascv1.ValidatePolicyResponse, error)
	propose     func(*ascv1.ProposePolicyRequest) (*ascv1.ProposePolicyResponse, error)
	evaluate    func(*ascv1.EvaluatePolicyRequest) (*ascv1.EvaluatePolicyResponse, error)
	auditList   func(*ascv1.ListAuditRequest) (*ascv1.ListAuditResponse, error)
	auditActs   func() (*ascv1.ListAuditActionsResponse, error)
	auditExport func(*ascv1.ExportAuditRequest, grpc.ServerStreamingServer[ascv1.ExportAuditResponse]) error
	auditChain  func(*ascv1.VerifyAuditChainRequest) (*ascv1.VerifyAuditChainResponse, error)
	listRules   func(*ascv1.ListSecretRulesRequest) (*ascv1.ListSecretRulesResponse, error)
	scan        func(*ascv1.ScanSecretsRequest) (*ascv1.ScanSecretsResponse, error)
	findings    func(*ascv1.ListSecretFindingsRequest) (*ascv1.ListSecretFindingsResponse, error)
	updateFind  func(*ascv1.UpdateSecretFindingRequest) (*ascv1.UpdateSecretFindingResponse, error)
	listKeys    func(*ascv1.ListSigningKeysRequest) (*ascv1.ListSigningKeysResponse, error)
	addKey      func(*ascv1.AddSigningKeyRequest) (*ascv1.AddSigningKeyResponse, error)
	revokeKey   func(*ascv1.RevokeSigningKeyRequest) (*ascv1.RevokeSigningKeyResponse, error)
	serverKey   func() (*ascv1.GetServerKeyResponse, error)
	verify      func(*ascv1.VerifySignaturesRequest) (*ascv1.VerifySignaturesResponse, error)
	listGroups  func(*ascv1.ListFailureGroupsRequest) (*ascv1.ListFailureGroupsResponse, error)
	getGroup    func(*ascv1.GetFailureGroupRequest) (*ascv1.GetFailureGroupResponse, error)
	setGroup    func(*ascv1.SetFailureGroupStateRequest) (*ascv1.SetFailureGroupStateResponse, error)
	history     func(*ascv1.FlakyHistoryRequest) (*ascv1.FlakyHistoryResponse, error)
	listFlaky   func(*ascv1.ListFlakyTestsRequest) (*ascv1.ListFlakyTestsResponse, error)
	depFindings func(*ascv1.ListDepFindingsRequest) (*ascv1.ListDepFindingsResponse, error)
	depUpdates  func(*ascv1.ListDepUpdatesRequest) (*ascv1.ListDepUpdatesResponse, error)
	depConfig   func(*ascv1.GetDepsConfigRequest) (*ascv1.GetDepsConfigResponse, error)
	depScan     func(*ascv1.ScanDepsRequest) (*ascv1.ScanDepsResponse, error)
	depPropose  func(*ascv1.ProposeDepUpdateRequest) (*ascv1.ProposeDepUpdateResponse, error)
	depDismiss  func(*ascv1.DismissDepFindingRequest) (*ascv1.DismissDepFindingResponse, error)
}

type fakeCall struct {
	Method string
	MD     metadata.MD
	Auth   string
	OpID   string
	Req    any
}

func (f *fakeAsc) note(ctx context.Context, method string, req any) {
	md, _ := metadata.FromIncomingContext(ctx)
	c := fakeCall{Method: method, Req: req, MD: md}
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

// last returns the last call of method.
func (f *fakeAsc) last(t *testing.T, method string) fakeCall {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := len(f.calls) - 1; i >= 0; i-- {
		if f.calls[i].Method == method {
			return f.calls[i]
		}
	}
	require.Failf(t, "no call", "%s was not called", method)
	return fakeCall{}
}

func (f *fakeAsc) count(method string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, c := range f.calls {
		if c.Method == method {
			n++
		}
	}
	return n
}

func (f *fakeAsc) Version(ctx context.Context, _ *ascv1.VersionRequest) (*ascv1.VersionResponse, error) {
	f.note(ctx, "Version", nil)
	if f.version != nil {
		return f.version(), nil
	}
	return &ascv1.VersionResponse{Features: []string{FeaturePolicy, FeatureAudit, FeatureSecretScan, FeatureSigning, FeatureCiInsights}}, nil
}

func (f *fakeAsc) GitMap(ctx context.Context, req *ascv1.GitMapRequest) (*ascv1.GitMapResponse, error) {
	f.note(ctx, "GitMap", req)
	if f.gitMap == nil {
		return f.UnimplementedRepoServer.GitMap(ctx, req)
	}
	return f.gitMap(req)
}

func (f *fakeAsc) GetSource(ctx context.Context, req *ascv1.GetPolicySourceRequest) (*ascv1.GetPolicySourceResponse, error) {
	f.note(ctx, "GetSource", req)
	if f.getSource == nil {
		return f.UnimplementedPolicyServer.GetSource(ctx, req)
	}
	return f.getSource(req)
}

func (f *fakeAsc) Validate(ctx context.Context, req *ascv1.ValidatePolicyRequest) (*ascv1.ValidatePolicyResponse, error) {
	f.note(ctx, "Validate", req)
	if f.validate == nil {
		return f.UnimplementedPolicyServer.Validate(ctx, req)
	}
	return f.validate(req)
}

func (f *fakeAsc) Propose(ctx context.Context, req *ascv1.ProposePolicyRequest) (*ascv1.ProposePolicyResponse, error) {
	f.note(ctx, "Propose", req)
	if f.propose == nil {
		return f.UnimplementedPolicyServer.Propose(ctx, req)
	}
	return f.propose(req)
}

func (f *fakeAsc) Evaluate(ctx context.Context, req *ascv1.EvaluatePolicyRequest) (*ascv1.EvaluatePolicyResponse, error) {
	f.note(ctx, "Evaluate", req)
	if f.evaluate == nil {
		return f.UnimplementedPolicyServer.Evaluate(ctx, req)
	}
	return f.evaluate(req)
}

func (f *fakeAsc) List(ctx context.Context, req *ascv1.ListAuditRequest) (*ascv1.ListAuditResponse, error) {
	f.note(ctx, "AuditList", req)
	if f.auditList == nil {
		return f.UnimplementedAuditServer.List(ctx, req)
	}
	return f.auditList(req)
}

func (f *fakeAsc) ListActions(ctx context.Context, req *ascv1.ListAuditActionsRequest) (*ascv1.ListAuditActionsResponse, error) {
	f.note(ctx, "AuditActions", req)
	if f.auditActs == nil {
		return f.UnimplementedAuditServer.ListActions(ctx, req)
	}
	return f.auditActs()
}

func (f *fakeAsc) Export(req *ascv1.ExportAuditRequest, stream grpc.ServerStreamingServer[ascv1.ExportAuditResponse]) error {
	f.note(stream.Context(), "AuditExport", req)
	if f.auditExport == nil {
		return f.UnimplementedAuditServer.Export(req, stream)
	}
	return f.auditExport(req, stream)
}

func (f *fakeAsc) VerifyChain(ctx context.Context, req *ascv1.VerifyAuditChainRequest) (*ascv1.VerifyAuditChainResponse, error) {
	f.note(ctx, "VerifyChain", req)
	if f.auditChain == nil {
		return f.UnimplementedAuditServer.VerifyChain(ctx, req)
	}
	return f.auditChain(req)
}

func (f *fakeAsc) ListRules(ctx context.Context, req *ascv1.ListSecretRulesRequest) (*ascv1.ListSecretRulesResponse, error) {
	f.note(ctx, "ListRules", req)
	if f.listRules == nil {
		return f.UnimplementedSecretScanServer.ListRules(ctx, req)
	}
	return f.listRules(req)
}

func (f *fakeAsc) Scan(ctx context.Context, req *ascv1.ScanSecretsRequest) (*ascv1.ScanSecretsResponse, error) {
	f.note(ctx, "Scan", req)
	if f.scan == nil {
		return f.UnimplementedSecretScanServer.Scan(ctx, req)
	}
	return f.scan(req)
}

func (f *fakeAsc) ListFindings(ctx context.Context, req *ascv1.ListSecretFindingsRequest) (*ascv1.ListSecretFindingsResponse, error) {
	f.note(ctx, "ListFindings", req)
	if f.findings == nil {
		return f.UnimplementedSecretScanServer.ListFindings(ctx, req)
	}
	return f.findings(req)
}

func (f *fakeAsc) UpdateFinding(ctx context.Context, req *ascv1.UpdateSecretFindingRequest) (*ascv1.UpdateSecretFindingResponse, error) {
	f.note(ctx, "UpdateFinding", req)
	if f.updateFind == nil {
		return f.UnimplementedSecretScanServer.UpdateFinding(ctx, req)
	}
	return f.updateFind(req)
}

func (f *fakeAsc) ListKeys(ctx context.Context, req *ascv1.ListSigningKeysRequest) (*ascv1.ListSigningKeysResponse, error) {
	f.note(ctx, "ListKeys", req)
	if f.listKeys == nil {
		return f.UnimplementedSigningServer.ListKeys(ctx, req)
	}
	return f.listKeys(req)
}

func (f *fakeAsc) AddKey(ctx context.Context, req *ascv1.AddSigningKeyRequest) (*ascv1.AddSigningKeyResponse, error) {
	f.note(ctx, "AddKey", req)
	if f.addKey == nil {
		return f.UnimplementedSigningServer.AddKey(ctx, req)
	}
	return f.addKey(req)
}

func (f *fakeAsc) RevokeKey(ctx context.Context, req *ascv1.RevokeSigningKeyRequest) (*ascv1.RevokeSigningKeyResponse, error) {
	f.note(ctx, "RevokeKey", req)
	if f.revokeKey == nil {
		return f.UnimplementedSigningServer.RevokeKey(ctx, req)
	}
	return f.revokeKey(req)
}

func (f *fakeAsc) GetServerKey(ctx context.Context, req *ascv1.GetServerKeyRequest) (*ascv1.GetServerKeyResponse, error) {
	f.note(ctx, "GetServerKey", req)
	if f.serverKey == nil {
		return f.UnimplementedSigningServer.GetServerKey(ctx, req)
	}
	return f.serverKey()
}

func (f *fakeAsc) Verify(ctx context.Context, req *ascv1.VerifySignaturesRequest) (*ascv1.VerifySignaturesResponse, error) {
	f.note(ctx, "Verify", req)
	if f.verify == nil {
		return f.UnimplementedSigningServer.Verify(ctx, req)
	}
	return f.verify(req)
}

func (f *fakeAsc) ListFailureGroups(ctx context.Context, req *ascv1.ListFailureGroupsRequest) (*ascv1.ListFailureGroupsResponse, error) {
	f.note(ctx, "ListFailureGroups", req)
	if f.listGroups == nil {
		return f.UnimplementedCiInsightsServer.ListFailureGroups(ctx, req)
	}
	return f.listGroups(req)
}

func (f *fakeAsc) GetFailureGroup(ctx context.Context, req *ascv1.GetFailureGroupRequest) (*ascv1.GetFailureGroupResponse, error) {
	f.note(ctx, "GetFailureGroup", req)
	if f.getGroup == nil {
		return f.UnimplementedCiInsightsServer.GetFailureGroup(ctx, req)
	}
	return f.getGroup(req)
}

func (f *fakeAsc) SetFailureGroupState(ctx context.Context, req *ascv1.SetFailureGroupStateRequest) (*ascv1.SetFailureGroupStateResponse, error) {
	f.note(ctx, "SetFailureGroupState", req)
	if f.setGroup == nil {
		return f.UnimplementedCiInsightsServer.SetFailureGroupState(ctx, req)
	}
	return f.setGroup(req)
}

func (f *fakeAsc) FlakyHistory(ctx context.Context, req *ascv1.FlakyHistoryRequest) (*ascv1.FlakyHistoryResponse, error) {
	f.note(ctx, "FlakyHistory", req)
	if f.history == nil {
		return f.UnimplementedCiInsightsServer.FlakyHistory(ctx, req)
	}
	return f.history(req)
}

func (f *fakeAsc) ListFlakyTests(ctx context.Context, req *ascv1.ListFlakyTestsRequest) (*ascv1.ListFlakyTestsResponse, error) {
	f.note(ctx, "ListFlakyTests", req)
	if f.listFlaky == nil {
		return f.UnimplementedCiInsightsServer.ListFlakyTests(ctx, req)
	}
	return f.listFlaky(req)
}

// fakeDeps is the DepsService of the fake: its method names meet those of the secret scanning service, so it is
// a type of its own. Its hooks are on fakeAsc.
type fakeDeps struct {
	ascv1.UnimplementedDepsServiceServer
	f *fakeAsc
}

func (d fakeDeps) ListFindings(ctx context.Context, req *ascv1.ListDepFindingsRequest) (*ascv1.ListDepFindingsResponse, error) {
	d.f.note(ctx, "DepFindings", req)
	if d.f.depFindings == nil {
		return d.UnimplementedDepsServiceServer.ListFindings(ctx, req)
	}
	return d.f.depFindings(req)
}

func (d fakeDeps) ListUpdates(ctx context.Context, req *ascv1.ListDepUpdatesRequest) (*ascv1.ListDepUpdatesResponse, error) {
	d.f.note(ctx, "DepUpdates", req)
	if d.f.depUpdates == nil {
		return d.UnimplementedDepsServiceServer.ListUpdates(ctx, req)
	}
	return d.f.depUpdates(req)
}

func (d fakeDeps) GetConfig(ctx context.Context, req *ascv1.GetDepsConfigRequest) (*ascv1.GetDepsConfigResponse, error) {
	d.f.note(ctx, "DepConfig", req)
	if d.f.depConfig == nil {
		return d.UnimplementedDepsServiceServer.GetConfig(ctx, req)
	}
	return d.f.depConfig(req)
}

func (d fakeDeps) Scan(ctx context.Context, req *ascv1.ScanDepsRequest) (*ascv1.ScanDepsResponse, error) {
	d.f.note(ctx, "DepScan", req)
	if d.f.depScan == nil {
		return d.UnimplementedDepsServiceServer.Scan(ctx, req)
	}
	return d.f.depScan(req)
}

func (d fakeDeps) ProposeUpdate(ctx context.Context, req *ascv1.ProposeDepUpdateRequest) (*ascv1.ProposeDepUpdateResponse, error) {
	d.f.note(ctx, "DepPropose", req)
	if d.f.depPropose == nil {
		return d.UnimplementedDepsServiceServer.ProposeUpdate(ctx, req)
	}
	return d.f.depPropose(req)
}

func (d fakeDeps) Dismiss(ctx context.Context, req *ascv1.DismissDepFindingRequest) (*ascv1.DismissDepFindingResponse, error) {
	d.f.note(ctx, "DepDismiss", req)
	if d.f.depDismiss == nil {
		return d.UnimplementedDepsServiceServer.Dismiss(ctx, req)
	}
	return d.f.depDismiss(req)
}

// startFake starts a fake asc serve on a loopback port and returns a client connected to it, with the internal
// token configured. The package connection (Conn) is pointed at it for the test.
func startFake(t *testing.T) (*fakeAsc, *ascclient.Client) {
	t.Helper()
	f := &fakeAsc{}
	srv := grpc.NewServer()
	ascv1.RegisterRepoServer(srv, f)
	ascv1.RegisterPolicyServer(srv, f)
	ascv1.RegisterAuditServer(srv, f)
	ascv1.RegisterSecretScanServer(srv, f)
	ascv1.RegisterSigningServer(srv, f)
	ascv1.RegisterCiInsightsServer(srv, f)
	ascv1.RegisterDepsServiceServer(srv, fakeDeps{f: f})
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	go func() { _ = srv.Serve(l) }()
	t.Cleanup(srv.Stop)
	c, err := ascclient.New(ascclient.Config{Target: l.Addr().String(), InternalToken: testInternalToken, Retry: ascclient.RetryConfig{MaxAttempts: 1}})
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Close() })
	t.Cleanup(UseConn(c))
	return f, c
}

// userCtx returns a context whose calls carry a user token, as UserCall would make them.
func userCtx(t *testing.T, token string) context.Context {
	t.Helper()
	ctx, err := ascclient.WithUserToken(context.Background(), token)
	require.NoError(t, err)
	return ctx
}
