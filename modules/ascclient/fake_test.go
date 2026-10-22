// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package ascclient

import (
	"context"
	"net"
	"sync"
	"testing"
	"time"

	ascv1 "forgejo.org/modules/ascproto/ascv1"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// seen is what the fake server saw of one call.
type seen struct {
	method string
	md     metadata.MD
}

// fakeRepo is an asc.v1.Repo server that follows scripted behaviour. It is the in-process
// stand-in for `asc serve`, so these tests need no Rust binary.
type fakeRepo struct {
	ascv1.UnimplementedRepoServer
	mu    sync.Mutex
	calls []seen

	// failures scripts the next errors of UpdateRefs and Version, consumed one per call.
	failures []error
	// trailer is sent with the next failure.
	trailer metadata.MD

	store map[string][]byte // "kind/hexid" -> stored bytes
	// getHook may rewrite what GetObjects is about to send; attempt counts GetObjects calls.
	getHook    func(attempt int, req *ascv1.GetObjectsRequest, send func(*ascv1.GetObjectsResponse) error) (handled bool, err error)
	getCalls   int
	getKeys    [][]*ascv1.ObjKey
	hasPresent []byte
	put        [][]*ascv1.Object
	putFail    []error
}

func (f *fakeRepo) record(ctx context.Context, method string) {
	md, _ := metadata.FromIncomingContext(ctx)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, seen{method: method, md: md})
}

func (f *fakeRepo) nextFailure(ctx context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.failures) == 0 {
		return nil
	}
	err := f.failures[0]
	f.failures = f.failures[1:]
	if f.trailer != nil {
		_ = grpc.SetTrailer(ctx, f.trailer)
	}
	return err
}

func (f *fakeRepo) seenCalls() []seen {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]seen(nil), f.calls...)
}

func (f *fakeRepo) Version(ctx context.Context, _ *ascv1.VersionRequest) (*ascv1.VersionResponse, error) {
	f.record(ctx, "Version")
	if err := f.nextFailure(ctx); err != nil {
		return nil, err
	}
	return &ascv1.VersionResponse{ServerVersion: "9.9.9", Features: []string{"search.v1", "cloud.v1"}}, nil
}

func (f *fakeRepo) UpdateRefs(ctx context.Context, _ *ascv1.UpdateRefsRequest) (*ascv1.UpdateRefsResponse, error) {
	f.record(ctx, "UpdateRefs")
	if err := f.nextFailure(ctx); err != nil {
		return nil, err
	}
	return &ascv1.UpdateRefsResponse{}, nil
}

func (f *fakeRepo) HasObjects(ctx context.Context, req *ascv1.HasObjectsRequest) (*ascv1.HasObjectsResponse, error) {
	f.record(ctx, "HasObjects")
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.hasPresent != nil {
		return &ascv1.HasObjectsResponse{Present: f.hasPresent}, nil
	}
	present := make([]byte, (len(req.GetKeys())+7)/8)
	for i := range req.GetKeys() {
		present[i/8] |= 1 << (i % 8) // everything present
	}
	return &ascv1.HasObjectsResponse{Present: present}, nil
}

func objectKeyName(k *ascv1.ObjKey) string {
	return k.GetKind().String() + "/" + string(k.GetId())
}

func (f *fakeRepo) GetObjects(stream grpc.BidiStreamingServer[ascv1.GetObjectsRequest, ascv1.GetObjectsResponse]) error {
	f.record(stream.Context(), "GetObjects")
	req, err := stream.Recv()
	if err != nil {
		return err
	}
	f.mu.Lock()
	f.getCalls++
	attempt := f.getCalls
	f.getKeys = append(f.getKeys, req.GetKeys())
	hook := f.getHook
	f.mu.Unlock()
	if len(req.GetKeys()) > MaxGetKeys {
		return status.Error(codes.InvalidArgument, "too many keys")
	}
	if hook != nil {
		handled, err := hook(attempt, req, stream.Send)
		if handled {
			return err
		}
	}
	resp := &ascv1.GetObjectsResponse{}
	f.mu.Lock()
	for _, k := range req.GetKeys() {
		if data, ok := f.store[objectKeyName(k)]; ok {
			resp.Objects = append(resp.Objects, &ascv1.Object{Key: k, Data: data})
		} else {
			resp.Missing = append(resp.Missing, k)
		}
	}
	f.mu.Unlock()
	return stream.Send(resp)
}

func (f *fakeRepo) PutObjects(stream grpc.ClientStreamingServer[ascv1.PutObjectsRequest, ascv1.PutObjectsResponse]) error {
	f.record(stream.Context(), "PutObjects")
	var batches [][]*ascv1.Object
	var n uint64
	for {
		req, err := stream.Recv()
		if err != nil {
			break
		}
		batches = append(batches, req.GetObjects())
		n += uint64(len(req.GetObjects()))
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.putFail) > 0 {
		err := f.putFail[0]
		f.putFail = f.putFail[1:]
		return err
	}
	f.put = append(f.put, batches...)
	return stream.SendAndClose(&ascv1.PutObjectsResponse{Stored: n, AlreadyPresent: 1})
}

type fakeServer struct {
	repo *fakeRepo
	addr string
}

// startFake serves fakeRepo on a loopback port until the test ends.
func startFake(t *testing.T) *fakeServer {
	t.Helper()
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	srv := grpc.NewServer()
	repo := &fakeRepo{store: map[string][]byte{}}
	ascv1.RegisterRepoServer(srv, repo)
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)
	return &fakeServer{repo: repo, addr: lis.Addr().String()}
}

// newTestClient dials the fake with fast retries.
func newTestClient(t *testing.T, s *fakeServer, mod func(*Config)) *Client {
	t.Helper()
	cfg := Config{
		Target:        s.addr,
		InternalToken: "internal-token",
		PoolSize:      2,
		CallTimeout:   5 * time.Second,
		StreamTimeout: 5 * time.Second,
		Retry:         RetryConfig{MaxAttempts: 5, Base: time.Millisecond, Cap: 20 * time.Millisecond},
	}
	if mod != nil {
		mod(&cfg)
	}
	c, err := New(cfg)
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Close() })
	return c
}

// primeBudget records n requests so the 10% retry budget allows retries.
func primeBudget(c *Client, n int) {
	for range n {
		c.retry.budget.record()
	}
}

func userCtx(t *testing.T, token string) context.Context {
	t.Helper()
	ctx, err := WithUserToken(t.Context(), token)
	require.NoError(t, err)
	return ctx
}
