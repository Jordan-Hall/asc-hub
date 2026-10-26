// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

// Package ascbyotest is an in-process stand-in for the Storage and AiProviders services of asc, for tests of
// the hub. It keeps targets, keys and migrations in memory, answers like asc does (credentials are accepted
// and never returned, a name is unique per owner, only a verified target can be enabled), records every
// call with its credentials metadata, and can be told to fail.
package ascbyotest

import (
	"context"
	"crypto/rand"
	"net"
	"strings"
	"sync"
	"testing"

	"forgejo.org/modules/ascclient"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/services/ascbyo"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// InternalToken is the token the test client sends; the fake refuses any other bearer.
const InternalToken = "test-internal-token"

// OpIDForTest is a valid op id for forms in tests.
const OpIDForTest = "0193f2a0-7b3c-7000-8000-000000000001"

// Call is one request the fake received.
type Call struct {
	Method string
	// Auth is the authorization metadata value.
	Auth string
	// OpID is the x-asc-op-id metadata value.
	OpID string
	Req  any
}

type target struct {
	pub               *ascv1.StorageTarget
	accessKey, secret string
	sessionToken      string
}

type key struct {
	pub    *ascv1.AiKey
	secret string
}

// Fake implements both services.
type Fake struct {
	ascv1.UnimplementedStorageServer
	ascv1.UnimplementedAiProvidersServer

	mu         sync.Mutex
	targets    map[string]*target
	keys       map[string]*key
	migrations map[string]*ascv1.StorageMigration
	calls      []Call
	// Fail maps a full method ("/asc.v1.Storage/CreateTarget") to the error it returns.
	Fail map[string]error
	// VerifyOK says whether VerifyTarget passes (default true).
	VerifyBad bool
	// InUse makes DeleteTarget answer STORAGE_IN_USE.
	InUse bool
	// TestStatus is the provider status TestKey answers with ("ok" by default).
	TestStatus string
	// UsageResp is returned by Usage.
	UsageResp *ascv1.AiUsageResponse
	// Models is returned by ListModels.
	Models []*ascv1.AiModel
	// SecretEcho makes error messages repeat the submitted secret, to prove the hub drops them.
	SecretEcho bool
	clock      int64
}

// Start runs the fake, points the hub at it and returns it.
func Start(t *testing.T) *Fake {
	t.Helper()
	f := &Fake{
		targets: map[string]*target{}, keys: map[string]*key{}, migrations: map[string]*ascv1.StorageMigration{},
		Fail: map[string]error{}, clock: 1_700_000_000,
	}
	srv := grpc.NewServer(grpc.UnaryInterceptor(f.intercept))
	ascv1.RegisterStorageServer(srv, f)
	ascv1.RegisterAiProvidersServer(srv, f)
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	go func() { _ = srv.Serve(l) }()
	t.Cleanup(srv.Stop)
	c, err := ascclient.New(ascclient.Config{Target: l.Addr().String(), InternalToken: InternalToken, Retry: ascclient.RetryConfig{MaxAttempts: 1}})
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Close() })
	t.Cleanup(ascbyo.UseAPI(ascbyo.API{Storage: ascv1.NewStorageClient(c), AI: ascv1.NewAiProvidersClient(c)}))
	return f
}

func (f *Fake) intercept(ctx context.Context, req any, info *grpc.UnaryServerInfo, h grpc.UnaryHandler) (any, error) {
	md, _ := metadata.FromIncomingContext(ctx)
	first := func(k string) string {
		if v := md.Get(k); len(v) > 0 {
			return v[0]
		}
		return ""
	}
	f.mu.Lock()
	f.calls = append(f.calls, Call{Method: info.FullMethod, Auth: first("authorization"), OpID: first("x-asc-op-id"), Req: req})
	fail := f.Fail[info.FullMethod]
	f.mu.Unlock()
	if first("authorization") != "Bearer "+InternalToken {
		return nil, status.Error(codes.Unauthenticated, "bad token")
	}
	if fail != nil {
		return nil, fail
	}
	return h(ctx, req)
}

// Calls returns a copy of the recorded calls.
func (f *Fake) Calls() []Call {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]Call(nil), f.calls...)
}

// CallsTo returns the calls of one method ("CreateTarget" matches "/asc.v1.Storage/CreateTarget").
func (f *Fake) CallsTo(method string) []Call {
	var out []Call
	for _, c := range f.Calls() {
		if strings.HasSuffix(c.Method, "/"+method) {
			out = append(out, c)
		}
	}
	return out
}

// Mutations counts the calls that change state.
func (f *Fake) Mutations() int {
	n := 0
	for _, c := range f.Calls() {
		m := c.Method[strings.LastIndex(c.Method, "/")+1:]
		if !strings.HasPrefix(m, "List") && !strings.HasPrefix(m, "Get") && m != "Usage" {
			n++
		}
	}
	return n
}

// Secrets returns everything secret the fake was given, for tests that grep pages and logs.
func (f *Fake) Secrets() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for _, t := range f.targets {
		out = append(out, t.accessKey, t.secret, t.sessionToken)
	}
	for _, k := range f.keys {
		out = append(out, k.secret)
	}
	return out
}

// SetFail makes method fail with err (nil clears it).
func (f *Fake) SetFail(method string, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err == nil {
		delete(f.Fail, method)
		return
	}
	f.Fail[method] = err
}

// Detail builds a gRPC error with an ErrorDetail.
func Detail(c codes.Code, msg, code string, paths ...string) error {
	st, err := status.New(c, msg).WithDetails(&ascv1.ErrorDetail{Code: code, Paths: paths})
	if err != nil {
		return status.Error(c, msg)
	}
	return st.Err()
}

func newID(prefix string) string {
	const alphabet = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, 20)
	_, _ = rand.Read(b)
	for i := range b {
		b[i] = alphabet[int(b[i])%len(alphabet)]
	}
	return prefix + string(b)
}

func (f *Fake) now() int64 { f.clock++; return f.clock }

func hint(s string) string {
	if len(s) <= 8 {
		return s
	}
	return s[:4] + "..." + s[len(s)-4:]
}

func sameOwner(a, b *ascv1.OwnerRef) bool {
	return a.GetKind() == b.GetKind() && strings.EqualFold(a.GetName(), b.GetName())
}

func (f *Fake) echo(msg, secret string) string {
	if f.SecretEcho {
		return msg + " " + secret
	}
	return msg
}

// AddTarget stores a target directly, in the given state, for tests that start from one.
func (f *Fake) AddTarget(owner *ascv1.OwnerRef, name string, state ascv1.StorageTargetState) *ascv1.StorageTarget {
	f.mu.Lock()
	defer f.mu.Unlock()
	t := &ascv1.StorageTarget{
		Id: newID("st_"), Owner: owner, Name: name, Endpoint: "https://s3.example.com", Region: "eu", Bucket: "bucket-" + name,
		AccessKeyHint: "AKIA...WXYZ", HasSecret: true, State: state, Created: f.now(), Updated: f.clock, KeyVersion: 1,
	}
	f.targets[t.Id] = &target{pub: t, accessKey: "AKIAPLANTEDKEYWXYZ", secret: "planted-secret-" + name}
	return t
}

// AddKey stores an AI key directly.
func (f *Fake) AddKey(owner *ascv1.OwnerRef, label string, p ascv1.AiProvider, state ascv1.AiKeyState) *ascv1.AiKey {
	f.mu.Lock()
	defer f.mu.Unlock()
	k := &ascv1.AiKey{
		Id: newID("ak_"), Owner: owner, Label: label, Provider: p, KeyHint: "wxyz", HasKey: true, State: state,
		Created: f.now(), Updated: f.clock, KeyVersion: 1,
	}
	f.keys[k.Id] = &key{pub: k, secret: "planted-key-" + label}
	return k
}

// AddMigration stores a migration directly.
func (f *Fake) AddMigration(m *ascv1.StorageMigration) *ascv1.StorageMigration {
	f.mu.Lock()
	defer f.mu.Unlock()
	if m.Id == "" {
		m.Id = newID("mg_")
	}
	if m.Created == 0 {
		m.Created = f.now()
	}
	f.migrations[m.Id] = m
	return m
}

// SetTargetFields changes a stored target in place.
func (f *Fake) SetTargetFields(id string, fn func(*ascv1.StorageTarget)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fn(f.targets[id].pub)
}

// SetKeyFields changes a stored key in place.
func (f *Fake) SetKeyFields(id string, fn func(*ascv1.AiKey)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fn(f.keys[id].pub)
}

// UpdateMigration changes a stored migration in place.
func (f *Fake) UpdateMigration(id string, fn func(*ascv1.StorageMigration)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fn(f.migrations[id])
}

// MigrationsOf returns the migrations of a repository.
func (f *Fake) MigrationsOf(repo string) []*ascv1.StorageMigration {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []*ascv1.StorageMigration
	for _, m := range f.migrations {
		if strings.EqualFold(m.GetRepo().GetName(), repo) {
			out = append(out, m)
		}
	}
	return out
}

// SetTargetState changes the state of a stored target.
func (f *Fake) SetTargetState(id string, st ascv1.StorageTargetState) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.targets[id].pub.State = st
}

// ---------------------------------------------------------------------------- Storage

func (f *Fake) CreateTarget(_ context.Context, r *ascv1.CreateStorageTargetRequest) (*ascv1.CreateStorageTargetResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	cr := r.GetCredentials()
	if r.GetOwner().GetKind() == ascv1.OwnerKind_OWNER_KIND_UNSPECIFIED || r.GetName() == "" || cr.GetAccessKeyId() == "" || cr.GetSecretAccessKey() == "" {
		return nil, status.Error(codes.InvalidArgument, f.echo("name, owner and credentials are required", cr.GetSecretAccessKey()))
	}
	if strings.HasPrefix(r.GetEndpoint(), "http://") {
		return nil, Detail(codes.InvalidArgument, "endpoint blocked", "STORAGE_ENDPOINT_BLOCKED", "not_https")
	}
	if strings.Contains(r.GetEndpoint(), "169.254.169.254") {
		return nil, Detail(codes.InvalidArgument, "endpoint blocked", "STORAGE_ENDPOINT_BLOCKED", "metadata")
	}
	for _, t := range f.targets {
		if sameOwner(t.pub.GetOwner(), r.GetOwner()) && t.pub.GetName() == r.GetName() {
			return nil, status.Error(codes.AlreadyExists, "name taken")
		}
	}
	t := &ascv1.StorageTarget{
		Id: newID("st_"), Owner: r.GetOwner(), Name: r.GetName(), Endpoint: r.GetEndpoint(), Region: r.GetRegion(), Bucket: r.GetBucket(),
		Prefix: r.GetPrefix(), Addressing: r.GetAddressing(), AccessKeyHint: hint(cr.GetAccessKeyId()), HasSecret: true,
		State: ascv1.StorageTargetState_STORAGE_TARGET_STATE_PENDING, Created: f.now(), Updated: f.clock, KeyVersion: 1,
	}
	f.targets[t.Id] = &target{pub: t, accessKey: cr.GetAccessKeyId(), secret: cr.GetSecretAccessKey(), sessionToken: cr.GetSessionToken()}
	return &ascv1.CreateStorageTargetResponse{Target: t}, nil
}

func (f *Fake) ListTargets(_ context.Context, r *ascv1.ListStorageTargetsRequest) (*ascv1.ListStorageTargetsResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := &ascv1.ListStorageTargetsResponse{}
	for _, t := range f.targets {
		if sameOwner(t.pub.GetOwner(), r.GetOwner()) {
			out.Targets = append(out.Targets, t.pub)
		}
	}
	return out, nil
}

// GetTarget answers for any owner: the fake is a system caller, so the hub must do the ownership check.
func (f *Fake) GetTarget(_ context.Context, r *ascv1.GetStorageTargetRequest) (*ascv1.GetStorageTargetResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	t := f.targets[r.GetId()]
	if t == nil {
		return nil, status.Error(codes.NotFound, "no such target")
	}
	return &ascv1.GetStorageTargetResponse{Target: t.pub}, nil
}

func (f *Fake) UpdateTarget(_ context.Context, r *ascv1.UpdateStorageTargetRequest) (*ascv1.UpdateStorageTargetResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	t := f.targets[r.GetId()]
	if t == nil {
		return nil, status.Error(codes.NotFound, "no such target")
	}
	reset := false
	if r.Name != nil {
		t.pub.Name = r.GetName()
	}
	if r.Endpoint != nil {
		t.pub.Endpoint, reset = r.GetEndpoint(), true
	}
	if r.Region != nil {
		t.pub.Region, reset = r.GetRegion(), true
	}
	if r.Bucket != nil {
		t.pub.Bucket, reset = r.GetBucket(), true
	}
	if r.Prefix != nil {
		t.pub.Prefix, reset = r.GetPrefix(), true
	}
	if r.Addressing != nil {
		t.pub.Addressing = r.GetAddressing()
	}
	if c := r.GetCredentials(); c != nil {
		t.accessKey, t.secret, t.sessionToken = c.GetAccessKeyId(), c.GetSecretAccessKey(), c.GetSessionToken()
		t.pub.AccessKeyHint, reset = hint(c.GetAccessKeyId()), true
	}
	if reset {
		t.pub.State = ascv1.StorageTargetState_STORAGE_TARGET_STATE_PENDING
	}
	t.pub.Updated = f.now()
	return &ascv1.UpdateStorageTargetResponse{Target: t.pub}, nil
}

func (f *Fake) DeleteTarget(_ context.Context, r *ascv1.DeleteStorageTargetRequest) (*ascv1.DeleteStorageTargetResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.targets[r.GetId()] == nil {
		return nil, status.Error(codes.NotFound, "no such target")
	}
	if f.InUse {
		return nil, Detail(codes.FailedPrecondition, "in use", "STORAGE_IN_USE")
	}
	delete(f.targets, r.GetId())
	return &ascv1.DeleteStorageTargetResponse{}, nil
}

func (f *Fake) VerifyTarget(_ context.Context, r *ascv1.VerifyStorageTargetRequest) (*ascv1.VerifyStorageTargetResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	t := f.targets[r.GetId()]
	if t == nil {
		return nil, status.Error(codes.NotFound, "no such target")
	}
	steps := []ascv1.StorageVerifyStep{
		ascv1.StorageVerifyStep_STORAGE_VERIFY_STEP_RESOLVE, ascv1.StorageVerifyStep_STORAGE_VERIFY_STEP_CONNECT,
		ascv1.StorageVerifyStep_STORAGE_VERIFY_STEP_PUT, ascv1.StorageVerifyStep_STORAGE_VERIFY_STEP_GET,
		ascv1.StorageVerifyStep_STORAGE_VERIFY_STEP_DELETE,
	}
	resp := &ascv1.VerifyStorageTargetResponse{Ok: !f.VerifyBad}
	for _, s := range steps {
		c := &ascv1.StorageVerifyCheck{Step: s, Ok: true, LatencyMs: 12}
		if f.VerifyBad && s == ascv1.StorageVerifyStep_STORAGE_VERIFY_STEP_PUT {
			c.Ok, c.Message = false, "access denied\x1b[31m"
		}
		resp.Checks = append(resp.Checks, c)
		if !c.Ok {
			break
		}
	}
	if f.VerifyBad {
		t.pub.State, t.pub.LastError = ascv1.StorageTargetState_STORAGE_TARGET_STATE_FAILED, "put: access denied"
	} else {
		t.pub.State, t.pub.LastError, t.pub.VerifiedAt = ascv1.StorageTargetState_STORAGE_TARGET_STATE_VERIFIED, "", f.now()
	}
	resp.Target = t.pub
	return resp, nil
}

func (f *Fake) SetEnabled(_ context.Context, r *ascv1.SetStorageTargetEnabledRequest) (*ascv1.SetStorageTargetEnabledResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	t := f.targets[r.GetId()]
	if t == nil {
		return nil, status.Error(codes.NotFound, "no such target")
	}
	st := t.pub.GetState()
	switch {
	case r.GetEnabled() && (st == ascv1.StorageTargetState_STORAGE_TARGET_STATE_VERIFIED || st == ascv1.StorageTargetState_STORAGE_TARGET_STATE_DISABLED):
		t.pub.State = ascv1.StorageTargetState_STORAGE_TARGET_STATE_ENABLED
	case !r.GetEnabled() && st == ascv1.StorageTargetState_STORAGE_TARGET_STATE_ENABLED:
		t.pub.State = ascv1.StorageTargetState_STORAGE_TARGET_STATE_DISABLED
	default:
		return nil, Detail(codes.FailedPrecondition, "verify the target first", "STORAGE_VERIFY_FAILED")
	}
	return &ascv1.SetStorageTargetEnabledResponse{Target: t.pub}, nil
}

func (f *Fake) AssignRepo(_ context.Context, r *ascv1.AssignRepoStorageRequest) (*ascv1.AssignRepoStorageResponse, error) {
	return &ascv1.AssignRepoStorageResponse{TargetId: r.GetTargetId()}, nil
}

func (f *Fake) StartMigration(_ context.Context, r *ascv1.StartStorageMigrationRequest) (*ascv1.StartStorageMigrationResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, m := range f.migrations {
		if strings.EqualFold(m.GetRepo().GetName(), r.GetRepo().GetName()) && m.GetState() < ascv1.StorageMigrationState_STORAGE_MIGRATION_STATE_DONE {
			return nil, Detail(codes.Aborted, "a migration is running", "MIGRATION_ACTIVE")
		}
	}
	m := &ascv1.StorageMigration{
		Id: newID("mg_"), Repo: r.GetRepo(), ToTargetId: r.GetToTargetId(), DeleteSource: r.GetDeleteSource(),
		State: ascv1.StorageMigrationState_STORAGE_MIGRATION_STATE_QUEUED, Created: f.now(), Updated: f.clock,
	}
	f.migrations[m.Id] = m
	return &ascv1.StartStorageMigrationResponse{Migration: m}, nil
}

func (f *Fake) GetMigration(_ context.Context, r *ascv1.GetStorageMigrationRequest) (*ascv1.GetStorageMigrationResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	m := f.migrations[r.GetId()]
	if m == nil {
		return nil, status.Error(codes.NotFound, "no such migration")
	}
	return &ascv1.GetStorageMigrationResponse{Migration: m}, nil
}

func (f *Fake) ListMigrations(_ context.Context, r *ascv1.ListStorageMigrationsRequest) (*ascv1.ListStorageMigrationsResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := &ascv1.ListStorageMigrationsResponse{}
	for _, m := range f.migrations {
		if strings.EqualFold(m.GetRepo().GetName(), r.GetRepo().GetName()) {
			out.Migrations = append(out.Migrations, m)
		}
	}
	return out, nil
}

func (f *Fake) CancelMigration(_ context.Context, r *ascv1.CancelStorageMigrationRequest) (*ascv1.CancelStorageMigrationResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	m := f.migrations[r.GetId()]
	if m == nil {
		return nil, status.Error(codes.NotFound, "no such migration")
	}
	if m.GetState() < ascv1.StorageMigrationState_STORAGE_MIGRATION_STATE_DONE {
		m.State = ascv1.StorageMigrationState_STORAGE_MIGRATION_STATE_CANCELLED
	}
	return &ascv1.CancelStorageMigrationResponse{Migration: m}, nil
}

// ---------------------------------------------------------------------------- AiProviders

func (f *Fake) CreateKey(_ context.Context, r *ascv1.CreateAiKeyRequest) (*ascv1.CreateAiKeyResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if r.GetLabel() == "" || r.GetApiKey() == "" || r.GetProvider() == ascv1.AiProvider_AI_PROVIDER_UNSPECIFIED {
		return nil, status.Error(codes.InvalidArgument, f.echo("label, provider and key are required", r.GetApiKey()))
	}
	if strings.Contains(r.GetBaseUrl(), "127.0.0.1") {
		return nil, Detail(codes.InvalidArgument, "blocked", "AI_ENDPOINT_BLOCKED", "loopback")
	}
	k := &ascv1.AiKey{
		Id: newID("ak_"), Owner: r.GetOwner(), Label: r.GetLabel(), Provider: r.GetProvider(), BaseUrl: r.GetBaseUrl(),
		DefaultModel: r.GetDefaultModel(), KeyHint: r.GetApiKey()[max(0, len(r.GetApiKey())-4):], HasKey: true,
		State: ascv1.AiKeyState_AI_KEY_STATE_UNTESTED, MonthlyCapMicroUsd: r.GetMonthlyCapMicroUsd(), Created: f.now(), Updated: f.clock, KeyVersion: 1,
	}
	f.keys[k.Id] = &key{pub: k, secret: r.GetApiKey()}
	return &ascv1.CreateAiKeyResponse{Key: k}, nil
}

func (f *Fake) ListKeys(_ context.Context, r *ascv1.ListAiKeysRequest) (*ascv1.ListAiKeysResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := &ascv1.ListAiKeysResponse{}
	for _, k := range f.keys {
		if sameOwner(k.pub.GetOwner(), r.GetOwner()) {
			out.Keys = append(out.Keys, k.pub)
		}
	}
	return out, nil
}

func (f *Fake) GetKey(_ context.Context, r *ascv1.GetAiKeyRequest) (*ascv1.GetAiKeyResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	k := f.keys[r.GetId()]
	if k == nil {
		return nil, status.Error(codes.NotFound, "no such key")
	}
	return &ascv1.GetAiKeyResponse{Key: k.pub}, nil
}

func (f *Fake) UpdateKey(_ context.Context, r *ascv1.UpdateAiKeyRequest) (*ascv1.UpdateAiKeyResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	k := f.keys[r.GetId()]
	if k == nil {
		return nil, status.Error(codes.NotFound, "no such key")
	}
	if r.Label != nil {
		k.pub.Label = r.GetLabel()
	}
	if r.ApiKey != nil {
		k.secret, k.pub.KeyHint = r.GetApiKey(), r.GetApiKey()[max(0, len(r.GetApiKey())-4):]
		k.pub.State = ascv1.AiKeyState_AI_KEY_STATE_UNTESTED
	}
	if r.BaseUrl != nil {
		k.pub.BaseUrl, k.pub.State = r.GetBaseUrl(), ascv1.AiKeyState_AI_KEY_STATE_UNTESTED
	}
	if r.DefaultModel != nil {
		k.pub.DefaultModel = r.GetDefaultModel()
	}
	if r.MonthlyCapMicroUsd != nil {
		k.pub.MonthlyCapMicroUsd = r.GetMonthlyCapMicroUsd()
	}
	if r.Disabled != nil {
		if r.GetDisabled() {
			k.pub.State = ascv1.AiKeyState_AI_KEY_STATE_DISABLED
		} else {
			k.pub.State = ascv1.AiKeyState_AI_KEY_STATE_ACTIVE
		}
	}
	k.pub.Updated = f.now()
	return &ascv1.UpdateAiKeyResponse{Key: k.pub}, nil
}

func (f *Fake) DeleteKey(_ context.Context, r *ascv1.DeleteAiKeyRequest) (*ascv1.DeleteAiKeyResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.keys[r.GetId()] == nil {
		return nil, status.Error(codes.NotFound, "no such key")
	}
	delete(f.keys, r.GetId())
	return &ascv1.DeleteAiKeyResponse{}, nil
}

func (f *Fake) TestKey(_ context.Context, r *ascv1.TestAiKeyRequest) (*ascv1.TestAiKeyResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	k := f.keys[r.GetId()]
	if k == nil {
		return nil, status.Error(codes.NotFound, "no such key")
	}
	st := f.TestStatus
	if st == "" {
		st = "ok"
	}
	resp := &ascv1.TestAiKeyResponse{Ok: st == "ok", LatencyMs: 80, ProviderStatus: st, Message: "provider said " + st}
	if resp.Ok {
		k.pub.State, k.pub.TestedAt, k.pub.LastError = ascv1.AiKeyState_AI_KEY_STATE_ACTIVE, f.now(), ""
	} else {
		k.pub.State, k.pub.LastError = ascv1.AiKeyState_AI_KEY_STATE_FAILED, st
	}
	resp.Key = k.pub
	return resp, nil
}

func (f *Fake) Usage(_ context.Context, _ *ascv1.AiUsageRequest) (*ascv1.AiUsageResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.UsageResp != nil {
		return f.UsageResp, nil
	}
	return &ascv1.AiUsageResponse{Total: &ascv1.AiUsageRow{}}, nil
}

func (f *Fake) ListModels(_ context.Context, _ *ascv1.ListAiModelsRequest) (*ascv1.ListAiModelsResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return &ascv1.ListAiModelsResponse{Models: f.Models}, nil
}
