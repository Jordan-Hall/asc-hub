// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package ascforward

import (
	"bytes"
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	ascv1 "forgejo.org/modules/ascproto/ascv1"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
)

// fakeAsc is an asc serve: a real gRPC server that keeps the metadata of each call.
type fakeAsc struct {
	ascv1.UnimplementedRepoServer
	ascv1.UnimplementedChangesServer
	ascv1.UnimplementedAiProvidersServer

	mu    sync.Mutex
	calls []metadata.MD
	block chan struct{}
}

func (f *fakeAsc) note(ctx context.Context) {
	md, _ := metadata.FromIncomingContext(ctx)
	f.mu.Lock()
	f.calls = append(f.calls, md)
	f.mu.Unlock()
}

func (f *fakeAsc) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

func (f *fakeAsc) last() metadata.MD {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls[len(f.calls)-1]
}

func (f *fakeAsc) ListRefs(ctx context.Context, in *ascv1.ListRefsRequest) (*ascv1.ListRefsResponse, error) {
	f.note(ctx)
	if f.block != nil {
		select {
		case <-f.block:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	switch in.GetRepo().GetName() {
	case "missing/repo":
		return nil, status.Error(codes.NotFound, "no such repository")
	case "private/repo":
		return nil, status.Error(codes.PermissionDenied, "not allowed")
	}
	trailer := metadata.Pairs("x-asc-trailer", "kept")
	_ = grpc.SetTrailer(ctx, trailer)
	return &ascv1.ListRefsResponse{Refs: []*ascv1.Ref{{Name: "refs/heads/master"}}}, nil
}

func (f *fakeAsc) WatchRefs(in *ascv1.WatchRefsRequest, stream grpc.ServerStreamingServer[ascv1.RefEvent]) error {
	f.note(stream.Context())
	for i := range 3 {
		if err := stream.Send(&ascv1.RefEvent{}); err != nil {
			return err
		}
		_ = i
	}
	return status.Error(codes.OutOfRange, "end of the watch")
}

func (f *fakeAsc) RepoAdmin(ctx context.Context, _ *ascv1.RepoAdminRequest) (*ascv1.RepoAdminResponse, error) {
	f.note(ctx)
	return &ascv1.RepoAdminResponse{}, nil
}

func (f *fakeAsc) ImportGitPush(ctx context.Context, _ *ascv1.ImportGitPushRequest) (*ascv1.ImportGitPushResponse, error) {
	f.note(ctx)
	return &ascv1.ImportGitPushResponse{}, nil
}

func (f *fakeAsc) ListKeys(ctx context.Context, _ *ascv1.ListAiKeysRequest) (*ascv1.ListAiKeysResponse, error) {
	f.note(ctx)
	return &ascv1.ListAiKeysResponse{}, nil
}

// rig is the whole path of a call: a gRPC client, the hub (an h2c server around the forwarder), asc serve.
type rig struct {
	asc  *fakeAsc
	hub  *httptest.Server
	conn *grpc.ClientConn
}

func newRig(t *testing.T) *rig {
	t.Helper()
	asc := &fakeAsc{}
	srv := grpc.NewServer()
	ascv1.RegisterRepoServer(srv, asc)
	ascv1.RegisterChangesServer(srv, asc)
	ascv1.RegisterAiProvidersServer(srv, asc)
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	go func() { _ = srv.Serve(l) }()
	t.Cleanup(srv.Stop)

	fwd, err := New("http://"+l.Addr().String(), nil)
	require.NoError(t, err)
	// The router of the hub: the forwarder takes POSTs of the asc.v1 paths, the rest is the web interface, which must
	// never see a call that the forwarder refuses.
	mux := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/asc.v1.") {
			fwd.ServeHTTP(w, r)
			return
		}
		http.Error(w, "the web interface", http.StatusTeapot)
	})
	hub := httptest.NewUnstartedServer(h2c.NewHandler(mux, &http2.Server{}))
	hub.Start()
	t.Cleanup(hub.Close)

	conn, err := grpc.NewClient(strings.TrimPrefix(hub.URL, "http://"), grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	return &rig{asc: asc, hub: hub, conn: conn}
}

func TestACloneStyleCallGoesThroughTheHub(t *testing.T) {
	r := newRig(t)
	ctx := metadata.AppendToOutgoingContext(t.Context(), "authorization", "Bearer the-clients-token", "x-asc-op-id", "op-1", "x-asc-client", "asc/1.0")
	var trailer metadata.MD
	resp, err := ascv1.NewRepoClient(r.conn).ListRefs(ctx, &ascv1.ListRefsRequest{Repo: &ascv1.RepoRef{Name: "acme/app"}}, grpc.Trailer(&trailer))
	require.NoError(t, err)
	require.Len(t, resp.GetRefs(), 1)
	assert.Equal(t, "refs/heads/master", resp.GetRefs()[0].GetName())
	assert.Equal(t, []string{"kept"}, trailer.Get("x-asc-trailer"), "trailers go through")

	md := r.asc.last()
	assert.Equal(t, []string{"Bearer the-clients-token"}, md.Get("authorization"), "the credential of the client goes on as it is")
	assert.Equal(t, []string{"op-1"}, md.Get("x-asc-op-id"))
	assert.Equal(t, []string{"asc/1.0"}, md.Get("x-asc-client"))
}

func TestStatusesOfAscReachTheClient(t *testing.T) {
	r := newRig(t)
	client := ascv1.NewRepoClient(r.conn)
	_, err := client.ListRefs(t.Context(), &ascv1.ListRefsRequest{Repo: &ascv1.RepoRef{Name: "missing/repo"}})
	assert.Equal(t, codes.NotFound, status.Code(err))
	_, err = client.ListRefs(t.Context(), &ascv1.ListRefsRequest{Repo: &ascv1.RepoRef{Name: "private/repo"}})
	assert.Equal(t, codes.PermissionDenied, status.Code(err))
}

func TestAServerStreamIsForwardedMessageByMessage(t *testing.T) {
	r := newRig(t)
	stream, err := ascv1.NewRepoClient(r.conn).WatchRefs(t.Context(), &ascv1.WatchRefsRequest{Repo: &ascv1.RepoRef{Name: "acme/app"}})
	require.NoError(t, err)
	n := 0
	for {
		_, err := stream.Recv()
		if err != nil {
			assert.Equal(t, codes.OutOfRange, status.Code(err), "the final status comes in the trailers")
			break
		}
		n++
	}
	assert.Equal(t, 3, n)
}

// A client cannot make the hub, or asc serve, believe a claim that only the hub signs, or pose as another address.
func TestHeadersAClientMustNotSendAreRemoved(t *testing.T) {
	r := newRig(t)
	ctx := metadata.AppendToOutgoingContext(t.Context(),
		"authorization", "Bearer t",
		"x-asc-forwarded-user", "1:root", "x-asc-forwarded-role", "admin", "x-asc-forwarded-ts", "1", "x-asc-forwarded-mac", "00",
		"x-asc-hub-user", "1", "x-asc-hub-admin", "1", "x-asc-hub-sig", "00",
		"x-forwarded-for", "10.9.9.9", "x-real-ip", "10.9.9.9", "forwarded", "for=10.9.9.9", "cookie", "session=stolen", "origin", "https://evil.example")
	_, err := ascv1.NewRepoClient(r.conn).ListRefs(ctx, &ascv1.ListRefsRequest{Repo: &ascv1.RepoRef{Name: "acme/app"}})
	require.NoError(t, err)
	md := r.asc.last()
	for _, k := range []string{"x-asc-forwarded-user", "x-asc-forwarded-role", "x-asc-forwarded-ts", "x-asc-forwarded-mac", "x-asc-hub-user", "x-asc-hub-admin", "x-asc-hub-sig", "x-real-ip", "forwarded", "cookie", "origin"} {
		assert.Empty(t, md.Get(k), k)
	}
	assert.Equal(t, []string{"127.0.0.1"}, md.Get("x-forwarded-for"), "the address the hub saw, not the one the client claims")
	assert.Equal(t, []string{"Bearer t"}, md.Get("authorization"))
}

func TestOnlyTheAllowedCallsAreForwarded(t *testing.T) {
	r := newRig(t)
	// The hub's own calls and the services that hold keys never reach asc serve.
	_, err := ascv1.NewRepoClient(r.conn).RepoAdmin(t.Context(), &ascv1.RepoAdminRequest{Repo: &ascv1.RepoRef{Name: "acme/app"}})
	assert.Equal(t, codes.Unimplemented, status.Code(err))
	_, err = ascv1.NewRepoClient(r.conn).ImportGitPush(t.Context(), &ascv1.ImportGitPushRequest{Repo: &ascv1.RepoRef{Name: "acme/app"}, GitDir: "/etc"})
	assert.Equal(t, codes.Unimplemented, status.Code(err))
	_, err = ascv1.NewAiProvidersClient(r.conn).ListKeys(t.Context(), &ascv1.ListAiKeysRequest{})
	assert.Equal(t, codes.Unimplemented, status.Code(err))
	assert.Zero(t, r.asc.count(), "asc serve was not asked")

	// Not gRPC, not POST, not a path of the protocol: plain HTTP answers, and asc serve is not asked.
	do := func(method, path, contentType string) *http.Response {
		req, err := http.NewRequestWithContext(t.Context(), method, r.hub.URL+path, strings.NewReader(""))
		require.NoError(t, err)
		if contentType != "" {
			req.Header.Set("Content-Type", contentType)
		}
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		resp.Body.Close()
		return resp
	}
	assert.Equal(t, http.StatusUnsupportedMediaType, do(http.MethodPost, "/asc.v1.Repo/ListRefs", "application/json").StatusCode)
	assert.Equal(t, http.StatusUnsupportedMediaType, do(http.MethodPost, "/asc.v1.Repo/ListRefs", "").StatusCode)
	assert.Equal(t, http.StatusTeapot, do(http.MethodGet, "/asc.v1.Repo/ListRefs", "application/grpc").StatusCode, "a GET is the web interface's")
	assert.Equal(t, http.StatusNotFound, do(http.MethodPost, "/asc.v1.Repo/ListRefs/extra", "application/grpc").StatusCode)
	assert.Equal(t, http.StatusNotFound, do(http.MethodPost, "/asc.v1.Repo/../../etc", "application/grpc").StatusCode)
	assert.Equal(t, http.StatusNotFound, do(http.MethodPost, "/asc.v1.Repo/ListRefs?x=1", "application/grpc").StatusCode)
	assert.Equal(t, http.StatusNotFound, do(http.MethodPost, "/asc.v1.Repo/"+strings.Repeat("a", 300), "application/grpc").StatusCode)
	assert.Zero(t, r.asc.count())
}

func TestTheAnswerCarriesNoCookieAndNoCORS(t *testing.T) {
	h := http.Header{}
	h.Set("Set-Cookie", "a=b")
	h.Set("Access-Control-Allow-Origin", "*")
	h.Set("access-control-allow-credentials", "true")
	h.Set("Grpc-Status", "0")
	stripResponse(h)
	assert.Empty(t, h.Get("Set-Cookie"))
	assert.Empty(t, h.Get("Access-Control-Allow-Origin"))
	assert.Empty(t, h.Get("Access-Control-Allow-Credentials"))
	assert.Equal(t, "0", h.Get("Grpc-Status"))
}

func TestAnUnreachableAscIsAnUnavailableStatusWithoutDetails(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := l.Addr().String()
	require.NoError(t, l.Close()) // nothing listens there now

	fwd, err := New("http://"+addr, nil)
	require.NoError(t, err)
	hub := httptest.NewUnstartedServer(h2c.NewHandler(fwd, &http2.Server{}))
	hub.Start()
	defer hub.Close()
	conn, err := grpc.NewClient(strings.TrimPrefix(hub.URL, "http://"), grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	defer conn.Close()
	_, err = ascv1.NewRepoClient(conn).ListRefs(t.Context(), &ascv1.ListRefsRequest{Repo: &ascv1.RepoRef{Name: "a/b"}})
	assert.Equal(t, codes.Unavailable, status.Code(err))
	assert.NotContains(t, err.Error(), addr, "the address of asc serve is not revealed")
}

func TestTooLargeARequestIsRefused(t *testing.T) {
	r := newRig(t)
	big := bytes.Repeat([]byte("a"), MaxUnaryBody+1024)
	_, err := ascv1.NewRepoClient(r.conn).ListRefs(t.Context(), &ascv1.ListRefsRequest{Repo: &ascv1.RepoRef{Name: string(big)}}, grpc.MaxCallSendMsgSize(1<<30))
	require.Error(t, err)
	assert.Equal(t, 0, r.asc.count(), "asc serve did not get it")
}

func TestTheNumberOfCallsInFlightIsBounded(t *testing.T) {
	r := newRig(t)
	r.asc.block = make(chan struct{})
	fwd, err := New(r.hub.URL, nil) // only to read the limit: the real one is the rig's
	require.NoError(t, err)
	assert.Equal(t, MaxInFlight, cap(fwd.sem))

	// Fill the slots of a forwarder by hand and see that the next call is refused with a retryable status.
	full := &Forwarder{sem: make(chan struct{}, 1)}
	full.sem <- struct{}{}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/asc.v1.Repo/ListRefs", nil)
	req.Header.Set("Content-Type", "application/grpc")
	full.ServeHTTP(rec, req)
	assert.Equal(t, "14", rec.Header().Get("Grpc-Status"))
	assert.Equal(t, "5", rec.Header().Get("Retry-After"))
	close(r.asc.block)
}

func TestACallThatTakesTooLongIsCutOff(t *testing.T) {
	r := newRig(t)
	r.asc.block = make(chan struct{}) // asc never answers
	defer close(r.asc.block)
	ctx, cancel := context.WithTimeout(t.Context(), 300*time.Millisecond)
	defer cancel()
	_, err := ascv1.NewRepoClient(r.conn).ListRefs(ctx, &ascv1.ListRefsRequest{Repo: &ascv1.RepoRef{Name: "a/b"}})
	assert.Equal(t, codes.DeadlineExceeded, status.Code(err), "the deadline of the client reaches asc serve and ends the call")
}

// Every call of the protocol is classified: a service or method added to asc.v1 is refused until someone allows it.
func TestEveryCallOfTheProtocolIsClassified(t *testing.T) {
	seen := 0
	for _, s := range registeredServices(t) {
		_, allowed := services[s.name]
		_, denied := DeniedServices[s.name]
		require.True(t, allowed != denied, "service %s must be in exactly one of the allowed and the denied services", s.name)
		for _, m := range s.methods {
			seen++
			if denied {
				assert.False(t, Allowed(s.name, m), "%s/%s", s.name, m)
				continue
			}
			_, deniedMethod := DeniedMethods["asc.v1."+s.name+"/"+m]
			assert.Equal(t, !deniedMethod, Allowed(s.name, m), "%s/%s", s.name, m)
		}
	}
	assert.Greater(t, seen, 100, "the registry was read")
	for k := range DeniedMethods {
		svc, m, _ := strings.Cut(strings.TrimPrefix(k, "asc.v1."), "/")
		_, ok := lookup(svc, m)
		assert.True(t, ok, "%s is denied and is not a call", k)
	}
	assert.False(t, Allowed("Repo", "NoSuchMethod"))
	assert.False(t, Allowed("NoSuchService", "ListRefs"))
	assert.False(t, Allowed("Repo", "ListRefs/../RepoAdmin"))
}

func TestNewChecksTheAddress(t *testing.T) {
	for _, bad := range []string{"", "ftp://x", "http://", "http://u:p@x:1", "http://x:1?q=1", "x:1"} {
		_, err := New(bad, nil)
		assert.Error(t, err, bad)
	}
	_, err := New("http://127.0.0.1:7890", nil)
	assert.NoError(t, err)
}

type svcInfo struct {
	name    string
	methods []string
}

func registeredServices(t *testing.T) []svcInfo {
	t.Helper()
	var out []svcInfo
	protoregistry.GlobalFiles.RangeFilesByPackage("asc.v1", func(fd protoreflect.FileDescriptor) bool {
		for i := range fd.Services().Len() {
			sd := fd.Services().Get(i)
			s := svcInfo{name: string(sd.Name())}
			for j := range sd.Methods().Len() {
				s.methods = append(s.methods, string(sd.Methods().Get(j).Name()))
			}
			out = append(out, s)
		}
		return true
	})
	return out
}
