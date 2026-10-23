// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	"bytes"
	gocontext "context"
	"encoding/binary"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"

	"forgejo.org/models/db"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unittest"
	"forgejo.org/modules/ascclient"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/test"
	"forgejo.org/services/contexttest"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/dynamicpb"
)

const ascRPCTestToken = "internal-token-for-the-test"

// ascRPCTestDenied lists every asc.v1 RPC that the proxy refuses on purpose. The classification test
// fails on an RPC that is in neither this list nor ascRPCReadMethods, so a new RPC cannot be
// served by accident and cannot be forgotten either. The Workspace service is IPC only and
// refused as a whole.
var ascRPCTestDenied = map[string]string{
	"asc.v1.Changes/RequestMerge": "writes", "asc.v1.Changes/RestackChanges": "writes", "asc.v1.Changes/SetReviewState": "writes",
	"asc.v1.Cloud/GetWorkspace": "selected by owner and name, not by repository permission", "asc.v1.Cloud/ListWorkspaces": "same",
	"asc.v1.Cloud/Purge": "writes", "asc.v1.Cloud/UpdateWorkspace": "writes",
	"asc.v1.Projects/ProjectHashes": "not used by the islands",
	"asc.v1.Repo/Apply":             "writes", "asc.v1.Repo/Commit": "writes", "asc.v1.Repo/CopyClosure": "writes",
	"asc.v1.Repo/Merge": "writes", "asc.v1.Repo/PutObjects": "writes", "asc.v1.Repo/Rebase": "writes",
	"asc.v1.Repo/RepoAdmin": "writes", "asc.v1.Repo/UpdateRefs": "writes", "asc.v1.Repo/ImportGitPush": "writes",
	"asc.v1.Repo/Ingest":  "writes",
	"asc.v1.Repo/Archive": "Forgejo has its own archive download", "asc.v1.Repo/GetObjects": "object level",
	"asc.v1.Repo/HasObjects": "object level", "asc.v1.Repo/CheckAttr": "not used by the islands",
	"asc.v1.Repo/Classify": "not used by the islands", "asc.v1.Repo/FindLanding": "not used by the islands",
	"asc.v1.Repo/GitMap": "not used by the islands", "asc.v1.Repo/IsAncestor": "not used by the islands",
	"asc.v1.Repo/LastCommits": "not used by the islands", "asc.v1.Repo/ListRefs": "not used by the islands",
	"asc.v1.Repo/MergeBase": "not used by the islands", "asc.v1.Repo/ReadTree": "not used by the islands",
	"asc.v1.Repo/Resolve": "not used by the islands", "asc.v1.Repo/Stat": "not used by the islands",
}

func TestAscRpcProxyClassification(t *testing.T) {
	seen := map[string]bool{}
	protoregistry.GlobalFiles.RangeFilesByPackage("asc.v1", func(fd protoreflect.FileDescriptor) bool {
		for i := range fd.Services().Len() {
			sd := fd.Services().Get(i)
			for j := range sd.Methods().Len() {
				md := sd.Methods().Get(j)
				name := string(sd.FullName()) + "/" + string(md.Name())
				seen[name] = true
				_, allowed := ascRPCReadMethods[name]
				_, denied := ascRPCTestDenied[name]
				switch {
				case sd.Name() == "Workspace":
					assert.False(t, allowed, name)
				case allowed && denied:
					t.Errorf("%s is both allowed and denied", name)
				case !allowed && !denied:
					t.Errorf("%s is not classified: add it to ascRPCReadMethods or to the denied list", name)
				}
			}
		}
		return true
	})
	for name := range ascRPCReadMethods {
		assert.True(t, seen[name], "%s is allowed but is not an RPC", name)
		md, ok := ascRPCMethod(name)
		require.True(t, ok, name)
		assert.False(t, md.IsStreamingClient(), name)
	}
	for name := range ascRPCTestDenied {
		assert.True(t, seen[name], "%s is denied but is not an RPC", name)
	}
}

// fakeAsc is a stand-in for asc serve that answers with raw bytes.
type fakeAsc struct {
	mu    sync.Mutex
	calls []fakeAscCall
}

type fakeAscCall struct {
	method string
	auth   []string
	other  []string // any metadata key that is not one of the client's own
	req    []byte
}

func startFakeAsc(t *testing.T) (*fakeAsc, string) {
	t.Helper()
	f := &fakeAsc{}
	srv := grpc.NewServer(grpc.ForceServerCodecV2(ascRPCRawCodec{}), grpc.UnknownServiceHandler(func(_ any, stream grpc.ServerStream) error {
		method, _ := grpc.MethodFromServerStream(stream)
		md, _ := metadata.FromIncomingContext(stream.Context())
		var req ascRPCRaw
		if err := stream.RecvMsg(&req); err != nil {
			return err
		}
		call := fakeAscCall{method: method, auth: md.Get("authorization"), req: req.b}
		for k := range md {
			switch k {
			case "authorization", "x-asc-client", "user-agent", "content-type", ":authority", "te", "grpc-accept-encoding", "x-asc-op-id":
			default:
				call.other = append(call.other, k)
			}
		}
		f.mu.Lock()
		f.calls = append(f.calls, call)
		f.mu.Unlock()
		switch method {
		case "/asc.v1.Repo/Log":
			for _, s := range []string{"one", "two", "three"} {
				if err := stream.SendMsg(&ascRPCRaw{b: []byte(s)}); err != nil {
					return err
				}
			}
			return nil
		case "/asc.v1.Repo/Version":
			return stream.SendMsg(&ascRPCRaw{b: []byte("version")})
		case "/asc.v1.Repo/ReadChangeset":
			return stream.SendMsg(&ascRPCRaw{b: []byte("changeset")})
		case "/asc.v1.Repo/RepoConfig":
			return status.Error(codes.NotFound, "no review.toml at that revision")
		case "/asc.v1.Repo/Compare":
			return status.Error(codes.Internal, "sqlite error at /var/lib/asc/asc.db password=hunter2")
		case "/asc.v1.Repo/Blame":
			if err := stream.SendMsg(&ascRPCRaw{b: bytes.Repeat([]byte("x"), 600)}); err != nil {
				return err
			}
			return stream.SendMsg(&ascRPCRaw{b: bytes.Repeat([]byte("y"), 600)})
		}
		return status.Error(codes.Unimplemented, "no such method")
	}))
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	go func() { _ = srv.Serve(l) }()
	t.Cleanup(srv.Stop)
	return f, l.Addr().String()
}

func (f *fakeAsc) last(t *testing.T) fakeAscCall {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	require.NotEmpty(t, f.calls)
	return f.calls[len(f.calls)-1]
}

func (f *fakeAsc) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

// useAsc points the proxy at addr for the test.
func useAsc(t *testing.T, addr string) {
	t.Helper()
	t.Cleanup(test.MockVariableValue(&setting.Asc.Enabled, true))
	ascRPCConnMu.Lock()
	oldNew, oldConn := ascRPCNewConn, ascRPCConnVal
	ascRPCConnVal = nil
	ascRPCNewConn = func() (grpc.ClientConnInterface, error) {
		return ascclient.New(ascclient.Config{Target: addr, InternalToken: ascRPCTestToken, Retry: ascclient.RetryConfig{MaxAttempts: 1}})
	}
	ascRPCConnMu.Unlock()
	t.Cleanup(func() {
		ascRPCConnMu.Lock()
		if c, ok := ascRPCConnVal.(*ascclient.Client); ok {
			_ = c.Close()
		}
		ascRPCNewConn, ascRPCConnVal = oldNew, oldConn
		ascRPCConnMu.Unlock()
	})
}

// stubConn is a connection that fails every call with err.
type stubConn struct{ err error }

func (s stubConn) Invoke(gocontext.Context, string, any, any, ...grpc.CallOption) error { return s.err }

func (s stubConn) NewStream(gocontext.Context, *grpc.StreamDesc, string, ...grpc.CallOption) (grpc.ClientStream, error) {
	return nil, s.err
}

func useStub(t *testing.T, err error) {
	t.Helper()
	t.Cleanup(test.MockVariableValue(&setting.Asc.Enabled, true))
	ascRPCConnMu.Lock()
	oldNew, oldConn := ascRPCNewConn, ascRPCConnVal
	ascRPCConnVal = nil
	ascRPCNewConn = func() (grpc.ClientConnInterface, error) { return stubConn{err}, nil }
	ascRPCConnMu.Unlock()
	t.Cleanup(func() {
		ascRPCConnMu.Lock()
		ascRPCNewConn, ascRPCConnVal = oldNew, oldConn
		ascRPCConnMu.Unlock()
	})
}

func setStorageBackend(t *testing.T, repoID int64, backend string) {
	t.Helper()
	_, err := db.GetEngine(db.DefaultContext).ID(repoID).Cols("storage_backend").Update(&repo_model.Repository{StorageBackend: backend})
	require.NoError(t, err)
}

func grpcFrame(msg []byte) []byte {
	out := make([]byte, 5, 5+len(msg))
	binary.BigEndian.PutUint32(out[1:], uint32(len(msg)))
	return append(out, msg...)
}

type proxyResult struct {
	status   int
	contType string
	data     [][]byte
	trailer  map[string]string
	body     string
}

// callProxy runs one request through the handler as doerID (0 is anonymous).
func callProxy(t *testing.T, doerID int64, service, method string, body []byte, mutate ...func(*http.Request)) proxyResult {
	t.Helper()
	ctx, resp := contexttest.MockContext(t, "POST /-/asc/rpc/"+service+"/"+method)
	ctx.Req.Method = http.MethodPost
	ctx.Req.Header.Set("Content-Type", "application/grpc-web+proto")
	ctx.Req.Body = io.NopCloser(bytes.NewReader(body))
	chi.RouteContext(ctx).URLParams.Add("service", service)
	chi.RouteContext(ctx).URLParams.Add("method", method)
	if doerID != 0 {
		contexttest.LoadUser(t, ctx, doerID)
		ctx.IsSigned = true
	}
	for _, m := range mutate {
		m(ctx.Req)
	}
	AscRPCProxy(ctx)

	res := proxyResult{status: resp.Code, contType: resp.Header().Get("Content-Type"), trailer: map[string]string{}, body: resp.Body.String()}
	if res.status != http.StatusOK || res.contType != ascRPCContentType {
		return res
	}
	raw := resp.Body.Bytes()
	for len(raw) > 0 {
		require.GreaterOrEqual(t, len(raw), 5)
		n := int(binary.BigEndian.Uint32(raw[1:5]))
		require.GreaterOrEqual(t, len(raw), 5+n)
		payload := raw[5 : 5+n]
		if raw[0]&0x80 != 0 {
			for line := range strings.SplitSeq(string(payload), "\r\n") {
				if k, v, ok := strings.Cut(line, ": "); ok {
					res.trailer[k] = v
				}
			}
			require.Len(t, raw, 5+n, "nothing follows the trailer")
		} else {
			res.data = append(res.data, payload)
		}
		raw = raw[5+n:]
	}
	return res
}

func mustMarshal(t *testing.T, m proto.Message) []byte {
	t.Helper()
	b, err := proto.Marshal(m)
	require.NoError(t, err)
	return grpcFrame(b)
}

func logReq(t *testing.T, repo string) []byte {
	return mustMarshal(t, &ascv1.LogRequest{Repo: &ascv1.RepoRef{Name: repo}})
}

func TestAscRpcProxyServesReadableRepositories(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	fake, addr := startFakeAsc(t)
	useAsc(t, addr)
	setStorageBackend(t, 1, "asc")

	t.Run("stream, anonymous, canonical name and own credential only", func(t *testing.T) {
		res := callProxy(t, 0, "asc.v1.Repo", "Log", logReq(t, "User2/REPO1"), func(r *http.Request) {
			r.Header.Set("Authorization", "Bearer browser-secret")
			r.Header.Set("Cookie", "i_like_gitea=session-secret")
			r.Header.Set("X-Asc-Op-Id", "attacker")
		})
		assert.Equal(t, http.StatusOK, res.status)
		assert.Equal(t, [][]byte{[]byte("one"), []byte("two"), []byte("three")}, res.data)
		assert.Equal(t, "0", res.trailer["grpc-status"])

		call := fake.last(t)
		assert.Equal(t, "/asc.v1.Repo/Log", call.method)
		assert.Equal(t, []string{"Bearer " + ascRPCTestToken}, call.auth, "only the internal token is sent")
		assert.Empty(t, call.other, "no browser metadata is forwarded")
		var got ascv1.LogRequest
		require.NoError(t, proto.Unmarshal(call.req, &got))
		assert.Equal(t, "user2/repo1", got.GetRepo().GetName(), "the name is canonical")
	})

	t.Run("unary, signed in, the short service name", func(t *testing.T) {
		res := callProxy(t, 2, "Repo", "ReadChangeset", mustMarshal(t, &ascv1.ReadChangesetRequest{Repo: &ascv1.RepoRef{Name: "user2/repo1"}, Rev: "main"}))
		assert.Equal(t, [][]byte{[]byte("changeset")}, res.data)
		assert.Equal(t, "0", res.trailer["grpc-status"])
	})

	t.Run("a nested repository reference is checked too", func(t *testing.T) {
		res := callProxy(t, 0, "asc.v1.CodeIntel", "Search",
			mustMarshal(t, &ascv1.SearchRequest{Scope: &ascv1.Scope{Repo: &ascv1.RepoRef{Name: "user2/repo2"}}, Pattern: "x"}))
		assert.Equal(t, "5", res.trailer["grpc-status"])
	})

	t.Run("asc errors keep a useful message", func(t *testing.T) {
		res := callProxy(t, 0, "asc.v1.Repo", "RepoConfig", mustMarshal(t, &ascv1.RepoConfigRequest{Repo: &ascv1.RepoRef{Name: "user2/repo1"}}))
		assert.Empty(t, res.data)
		assert.Equal(t, "5", res.trailer["grpc-status"])
		assert.Equal(t, "no review.toml at that revision", res.trailer["grpc-message"])
	})

	t.Run("an internal asc error is replaced", func(t *testing.T) {
		res := callProxy(t, 0, "asc.v1.Repo", "Compare", mustMarshal(t, &ascv1.CompareRequest{Repo: &ascv1.RepoRef{Name: "user2/repo1"}}))
		assert.Equal(t, "13", res.trailer["grpc-status"])
		assert.Equal(t, "internal error", res.trailer["grpc-message"])
		assert.NotContains(t, res.body, "hunter2")
		assert.NotContains(t, res.body, "sqlite")
	})
}

func TestAscRpcProxyHidesUnreadableRepositories(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	fake, addr := startFakeAsc(t)
	useAsc(t, addr)
	setStorageBackend(t, 1, "asc")
	setStorageBackend(t, 2, "asc") // user2/repo2 is private
	setStorageBackend(t, 4, "git")

	notFound := func(t *testing.T, res proxyResult) {
		t.Helper()
		assert.Equal(t, http.StatusOK, res.status)
		assert.Empty(t, res.data)
		assert.Equal(t, "5", res.trailer["grpc-status"])
		assert.Equal(t, "repository not found", res.trailer["grpc-message"])
	}
	before := fake.count()

	missing := callProxy(t, 0, "asc.v1.Repo", "Log", logReq(t, "user2/does-not-exist"))
	notFound(t, missing)

	for name, doer := range map[string]int64{"anonymous": 0, "other user": 4} {
		t.Run("private repository, "+name, func(t *testing.T) {
			res := callProxy(t, doer, "asc.v1.Repo", "Log", logReq(t, "user2/repo2"))
			notFound(t, res)
			assert.Equal(t, missing.body, res.body, "same bytes as a repository that does not exist")
		})
	}
	t.Run("public repository that is not stored in asc", func(t *testing.T) {
		res := callProxy(t, 0, "asc.v1.Repo", "Log", logReq(t, "user5/repo4"))
		notFound(t, res)
		assert.Equal(t, missing.body, res.body)
	})
	t.Run("owner can read the private repository", func(t *testing.T) {
		res := callProxy(t, 2, "asc.v1.Repo", "Log", logReq(t, "user2/repo2"))
		assert.Equal(t, "0", res.trailer["grpc-status"])
	})
	t.Run("a path or odd name", func(t *testing.T) {
		for _, name := range []string{"", "user2", "user2/", "/repo1", "user2/repo1/x", "user2/repo1.git", "../user2/repo1", strings.Repeat("a", 300) + "/b"} {
			notFound(t, callProxy(t, 0, "asc.v1.Repo", "Log", logReq(t, name)))
		}
	})
	t.Run("signed-in but prohibited user", func(t *testing.T) {
		ctx, _ := contexttest.MockContext(t, "POST /x")
		contexttest.LoadUser(t, ctx, 2)
		ctx.Doer.ProhibitLogin = true
		_, err := ascRPCCanonicalName(ctx, "user2/repo1")
		assert.ErrorIs(t, err, errAscRPCNotFound)
	})
	assert.Equal(t, before+1, fake.count(), "only the owner's call reached asc")
}

func TestAscRpcProxyRefusesBadRequests(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	fake, addr := startFakeAsc(t)
	useAsc(t, addr)
	setStorageBackend(t, 1, "asc")
	good := logReq(t, "user2/repo1")

	t.Run("disabled", func(t *testing.T) {
		defer test.MockVariableValue(&setting.Asc.Enabled, false)()
		assert.Equal(t, http.StatusNotFound, callProxy(t, 0, "asc.v1.Repo", "Log", good).status)
	})
	t.Run("methods that are not on the allowlist", func(t *testing.T) {
		for _, m := range [][2]string{
			{"asc.v1.Repo", "UpdateRefs"}, {"asc.v1.Repo", "Commit"}, {"asc.v1.Repo", "GetObjects"}, {"asc.v1.Repo", "PutObjects"},
			{"asc.v1.Cloud", "GetWorkspace"}, {"asc.v1.Workspace", "Status"}, {"asc.v1.Repo", "log"}, {"asc.v1.Repo", "Nope"},
			{"asc.v1.Repo", "../Log"}, {"x", "Log"}, {"asc.v1.", "Log"},
		} {
			assert.Equal(t, http.StatusNotFound, callProxy(t, 0, m[0], m[1], good).status, "%v", m)
		}
	})
	t.Run("not POST", func(t *testing.T) {
		res := callProxy(t, 0, "asc.v1.Repo", "Log", good, func(r *http.Request) { r.Method = http.MethodGet })
		assert.Equal(t, http.StatusMethodNotAllowed, res.status)
	})
	t.Run("content types", func(t *testing.T) {
		for _, ct := range []string{"", "application/json", "application/grpc", "application/grpc-web-text", "application/grpc-web-text+proto", "text/plain"} {
			res := callProxy(t, 0, "asc.v1.Repo", "Log", good, func(r *http.Request) { r.Header.Set("Content-Type", ct) })
			assert.Equal(t, http.StatusUnsupportedMediaType, res.status, ct)
		}
		res := callProxy(t, 0, "asc.v1.Repo", "Log", good, func(r *http.Request) { r.Header.Set("Content-Type", "application/grpc-web+proto; charset=utf-8") })
		assert.Equal(t, "0", res.trailer["grpc-status"])
		res = callProxy(t, 0, "asc.v1.Repo", "Log", good, func(r *http.Request) { r.Header.Set("Grpc-Encoding", "gzip") })
		assert.Equal(t, http.StatusUnsupportedMediaType, res.status)
	})
	t.Run("framing", func(t *testing.T) {
		compressed := append([]byte(nil), good...)
		compressed[0] = 1
		trailerFlag := append([]byte(nil), good...)
		trailerFlag[0] = 0x80
		for name, body := range map[string][]byte{
			"empty": nil, "short": good[:3], "truncated": good[:len(good)-1], "trailing bytes": append(append([]byte(nil), good...), 0),
			"two frames": append(append([]byte(nil), good...), good...), "compressed": compressed, "trailer flag": trailerFlag,
		} {
			assert.Equal(t, http.StatusBadRequest, callProxy(t, 0, "asc.v1.Repo", "Log", body).status, name)
		}
		big := grpcFrame(bytes.Repeat([]byte{0}, ascRPCMaxRequestBytes+1))
		assert.Equal(t, http.StatusRequestEntityTooLarge, callProxy(t, 0, "asc.v1.Repo", "Log", big).status)
	})
	t.Run("message problems", func(t *testing.T) {
		unknown := protowire.AppendTag(nil, 99, protowire.VarintType)
		unknown = protowire.AppendVarint(unknown, 1)
		okMsg, err := proto.Marshal(&ascv1.LogRequest{Repo: &ascv1.RepoRef{Name: "user2/repo1"}})
		require.NoError(t, err)

		// An unknown field inside the RepoRef.
		inner := protowire.AppendTag(nil, 1, protowire.BytesType)
		inner = protowire.AppendString(inner, "user2/repo1")
		inner = append(inner, unknown...)
		nested := protowire.AppendTag(nil, 1, protowire.BytesType)
		nested = protowire.AppendBytes(nested, inner)

		for name, body := range map[string][]byte{
			"garbage":         grpcFrame([]byte{0xff, 0xff, 0xff}),
			"unknown field":   grpcFrame(append(append([]byte(nil), okMsg...), unknown...)),
			"unknown in ref":  grpcFrame(nested),
			"no repository":   mustMarshal(t, &ascv1.LogRequest{}),
			"empty ref":       mustMarshal(t, &ascv1.LogRequest{Repo: &ascv1.RepoRef{}}),
			"invalid utf-8":   grpcFrame(append(protowire.AppendTag(nil, 1, protowire.BytesType), protowire.AppendBytes(nil, append(protowire.AppendTag(nil, 1, protowire.BytesType), protowire.AppendBytes(nil, []byte{0xff, '/', 'a'})...))...)),
			"wrong wire type": grpcFrame(protowire.AppendVarint(protowire.AppendTag(nil, 1, protowire.VarintType), 7)),
		} {
			before := fake.count()
			res := callProxy(t, 0, "asc.v1.Repo", "Log", body)
			assert.Equal(t, http.StatusOK, res.status, name)
			assert.NotEqual(t, "0", res.trailer["grpc-status"], name)
			assert.NotEmpty(t, res.trailer["grpc-status"], name)
			assert.Equal(t, before, fake.count(), "%s: nothing reached asc", name)
		}
	})
	t.Run("Version needs no repository", func(t *testing.T) {
		res := callProxy(t, 0, "asc.v1.Repo", "Version", grpcFrame(nil))
		assert.Equal(t, [][]byte{[]byte("version")}, res.data)
		assert.Equal(t, "0", res.trailer["grpc-status"])
	})
}

func TestAscRpcProxyResponseLimit(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	_, addr := startFakeAsc(t)
	useAsc(t, addr)
	setStorageBackend(t, 1, "asc")
	defer test.MockVariableValue(&ascRPCMaxResponseBytes, 1000)()

	// Two messages of 600 bytes: the first goes out, the second is over the cap.
	res := callProxy(t, 0, "asc.v1.Repo", "Blame", mustMarshal(t, &ascv1.BlameRequest{Repo: &ascv1.RepoRef{Name: "user2/repo1"}}))
	assert.Equal(t, "8", res.trailer["grpc-status"])
	assert.Equal(t, "response is too large", res.trailer["grpc-message"])
	assert.LessOrEqual(t, len(res.data), 1)
}

func TestAscRpcProxyErrorsDoNotLeak(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	setStorageBackend(t, 1, "asc")
	req := logReq(t, "user2/repo1")

	for _, tc := range []struct {
		name string
		err  error
		code string
		msg  string
	}{
		{"unavailable", status.Error(codes.Unavailable, `connection error: desc = "transport: Error while dialing: dial tcp 10.1.2.3:7890: connect: refused"`), "14", "asc is unavailable"},
		{"deadline", status.Error(codes.DeadlineExceeded, "context deadline exceeded 10.1.2.3"), "4", "deadline exceeded"},
		{"permission denied for the internal token", status.Error(codes.PermissionDenied, "token abc123 is not allowed"), "13", "internal error"},
		{"unauthenticated", status.Error(codes.Unauthenticated, "bad bearer abc123"), "13", "internal error"},
		{"unknown", status.Error(codes.Unknown, "panic at src/secret.rs"), "13", "internal error"},
		{"not a status", io.ErrUnexpectedEOF, "13", "internal error"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			useStub(t, tc.err)
			for _, method := range []string{"ReadChangeset", "Log"} { // unary and streaming
				body := req
				if method == "ReadChangeset" {
					body = mustMarshal(t, &ascv1.ReadChangesetRequest{Repo: &ascv1.RepoRef{Name: "user2/repo1"}})
				}
				res := callProxy(t, 0, "asc.v1.Repo", method, body)
				assert.Equal(t, http.StatusOK, res.status)
				assert.Equal(t, tc.code, res.trailer["grpc-status"], method)
				assert.Equal(t, tc.msg, res.trailer["grpc-message"], method)
				assert.NotContains(t, res.body, "abc123")
				assert.NotContains(t, res.body, "10.1.2.3")
			}
		})
	}

	t.Run("no connection", func(t *testing.T) {
		t.Cleanup(test.MockVariableValue(&setting.Asc.Enabled, true))
		ascRPCConnMu.Lock()
		oldNew, oldConn := ascRPCNewConn, ascRPCConnVal
		ascRPCConnVal = nil
		ascRPCNewConn = func() (grpc.ClientConnInterface, error) { return nil, io.ErrClosedPipe }
		ascRPCConnMu.Unlock()
		t.Cleanup(func() {
			ascRPCConnMu.Lock()
			ascRPCNewConn, ascRPCConnVal = oldNew, oldConn
			ascRPCConnMu.Unlock()
		})
		res := callProxy(t, 0, "asc.v1.Repo", "Log", req)
		assert.Equal(t, "14", res.trailer["grpc-status"])
		assert.Equal(t, "asc is unavailable", res.trailer["grpc-message"])
	})
}

func TestAscRpcProxyConcurrencyLimit(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	setStorageBackend(t, 1, "asc")
	useStub(t, status.Error(codes.NotFound, "x"))
	for range cap(ascRPCSlots) {
		ascRPCSlots <- struct{}{}
	}
	defer func() {
		for range cap(ascRPCSlots) {
			<-ascRPCSlots
		}
	}()
	res := callProxy(t, 0, "asc.v1.Repo", "Log", logReq(t, "user2/repo1"))
	assert.Equal(t, "8", res.trailer["grpc-status"])
}

func TestAscRpcProxyCollectRefs(t *testing.T) {
	// MergeRequest has two RepoRef fields. The proxy does not serve it, but the walk is generic and
	// must find both.
	md := (&ascv1.MergeRequest{}).ProtoReflect().Descriptor()
	m := dynamicpb.NewMessage(md)
	raw, err := proto.Marshal(&ascv1.MergeRequest{Repo: &ascv1.RepoRef{Name: "a/b"}, HeadRepo: &ascv1.RepoRef{Name: "c/d"}})
	require.NoError(t, err)
	require.NoError(t, proto.Unmarshal(raw, m))
	var refs []protoreflect.Message
	require.NoError(t, ascRPCCollectRefs(m, 0, &refs))
	var names []string
	for _, r := range refs {
		names = append(names, r.Get(r.Descriptor().Fields().ByName("name")).String())
	}
	assert.ElementsMatch(t, []string{"a/b", "c/d"}, names)

	// A chain of nested messages deeper than the limit is refused.
	t.Run("depth", func(t *testing.T) {
		scope := &ascv1.Scope{Repo: &ascv1.RepoRef{Name: "a/b"}}
		m := dynamicpb.NewMessage(scope.ProtoReflect().Descriptor())
		raw, err := proto.Marshal(scope)
		require.NoError(t, err)
		require.NoError(t, proto.Unmarshal(raw, m))
		var refs []protoreflect.Message
		assert.Error(t, ascRPCCollectRefs(m, ascRPCMaxDepth+1, &refs))
	})
}

func TestAscRpcProxyFrameAndMessageEncoding(t *testing.T) {
	msg, ok := ascRPCParseFrame(grpcFrame([]byte("abc")))
	assert.True(t, ok)
	assert.Equal(t, "abc", string(msg))
	_, ok = ascRPCParseFrame(grpcFrame(nil)[:4])
	assert.False(t, ok)

	assert.Equal(t, "plain text", ascRPCEncodeMessage("plain text"))
	assert.Equal(t, "100%25 done%0D%0A%C3%A9", ascRPCEncodeMessage("100% done\r\né"))
}
