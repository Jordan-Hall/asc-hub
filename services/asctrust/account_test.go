// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package asctrust

import (
	"bytes"
	"context"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"forgejo.org/modules/ascclient"
	ascv1 "forgejo.org/modules/ascproto/ascv1"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

// A fake Sessions and DataExport. The hooks are plain fields; a nil hook answers Unimplemented. Every request and the
// op id it carried are kept.
type fakeAccount struct {
	ascv1.UnimplementedSessionsServer
	ascv1.UnimplementedDataExportServer

	mu   sync.Mutex
	reqs []any
	ops  []string

	list     func(*ascv1.ListSessionsRequest) (*ascv1.ListSessionsResponse, error)
	revoke   func(*ascv1.RevokeSessionRequest) (*ascv1.RevokeSessionResponse, error)
	others   func(*ascv1.RevokeOtherSessionsRequest) (*ascv1.RevokeOtherSessionsResponse, error)
	exports  func() (*ascv1.ListExportsResponse, error)
	request  func() (*ascv1.RequestExportResponse, error)
	download func(*ascv1.DownloadExportRequest, grpc.ServerStreamingServer[ascv1.ExportChunk]) error
	del      func(*ascv1.DeleteExportRequest) (*ascv1.DeleteExportResponse, error)
}

func (f *fakeAccount) note(ctx context.Context, req any) {
	md, _ := metadata.FromIncomingContext(ctx)
	op := ""
	if v := md.Get("x-asc-op-id"); len(v) > 0 {
		op = v[0]
	}
	f.mu.Lock()
	f.reqs = append(f.reqs, req)
	f.ops = append(f.ops, op)
	f.mu.Unlock()
}

func (f *fakeAccount) ListSessions(ctx context.Context, r *ascv1.ListSessionsRequest) (*ascv1.ListSessionsResponse, error) {
	f.note(ctx, r)
	if f.list == nil {
		return f.UnimplementedSessionsServer.ListSessions(ctx, r)
	}
	return f.list(r)
}

func (f *fakeAccount) RevokeSession(ctx context.Context, r *ascv1.RevokeSessionRequest) (*ascv1.RevokeSessionResponse, error) {
	f.note(ctx, r)
	if f.revoke == nil {
		return f.UnimplementedSessionsServer.RevokeSession(ctx, r)
	}
	return f.revoke(r)
}

func (f *fakeAccount) RevokeOtherSessions(ctx context.Context, r *ascv1.RevokeOtherSessionsRequest) (*ascv1.RevokeOtherSessionsResponse, error) {
	f.note(ctx, r)
	if f.others == nil {
		return f.UnimplementedSessionsServer.RevokeOtherSessions(ctx, r)
	}
	return f.others(r)
}

func (f *fakeAccount) ListExports(ctx context.Context, r *ascv1.ListExportsRequest) (*ascv1.ListExportsResponse, error) {
	f.note(ctx, r)
	if f.exports == nil {
		return f.UnimplementedDataExportServer.ListExports(ctx, r)
	}
	return f.exports()
}

func (f *fakeAccount) RequestExport(ctx context.Context, r *ascv1.RequestExportRequest) (*ascv1.RequestExportResponse, error) {
	f.note(ctx, r)
	if f.request == nil {
		return f.UnimplementedDataExportServer.RequestExport(ctx, r)
	}
	return f.request()
}

func (f *fakeAccount) Download(r *ascv1.DownloadExportRequest, s grpc.ServerStreamingServer[ascv1.ExportChunk]) error {
	f.note(s.Context(), r)
	if f.download == nil {
		return f.UnimplementedDataExportServer.Download(r, s)
	}
	return f.download(r, s)
}

func (f *fakeAccount) DeleteExport(ctx context.Context, r *ascv1.DeleteExportRequest) (*ascv1.DeleteExportResponse, error) {
	f.note(ctx, r)
	if f.del == nil {
		return f.UnimplementedDataExportServer.DeleteExport(ctx, r)
	}
	return f.del(r)
}

func startAccountFake(t *testing.T) (*fakeAccount, *ascclient.Client) {
	t.Helper()
	f := &fakeAccount{}
	srv := grpc.NewServer()
	ascv1.RegisterSessionsServer(srv, f)
	ascv1.RegisterDataExportServer(srv, f)
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	go func() { _ = srv.Serve(l) }()
	t.Cleanup(srv.Stop)
	c, err := ascclient.New(ascclient.Config{Target: l.Addr().String(), InternalToken: testInternalToken, Retry: ascclient.RetryConfig{MaxAttempts: 1}})
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Close() })
	return f, c
}

func TestLoadSessionsCleansAndChecksTheAnswer(t *testing.T) {
	f, c := startAccountFake(t)
	f.list = func(r *ascv1.ListSessionsRequest) (*ascv1.ListSessionsResponse, error) {
		return &ascv1.ListSessionsResponse{Sessions: []*ascv1.Session{
			{Id: "tok:7", Kind: ascv1.SessionKind_SESSION_KIND_TOKEN, Label: "ci‮\x00 laptop\n", Scope: "acme/* write", Created: 1700000000, LastUsed: 0, Expires: 1800000000},
			{Id: "ssh:3", Kind: ascv1.SessionKind_SESSION_KIND_SSH_KEY, Label: "work", Created: 1700000100},
			{Id: "agt:9", Kind: ascv1.SessionKind_SESSION_KIND_AGENT_TOKEN, Label: "coder", Scope: "acme/web"},
		}}, nil
	}
	rows, err := LoadSessions(sysCtx(), c, "alice")
	require.NoError(t, err)
	require.Len(t, rows, 3)
	assert.Equal(t, "alice", f.reqs[0].(*ascv1.ListSessionsRequest).GetLogin())
	assert.Equal(t, "ci laptop", rows[0].Label, "control and bidirectional characters are removed")
	assert.Equal(t, "token", rows[0].Kind)
	assert.True(t, rows[0].LastUsed.IsZero(), "0 is never")
	assert.Equal(t, time.Unix(1800000000, 0).UTC(), rows[0].Expires)
	assert.Equal(t, []string{"token", "ssh", "agent"}, []string{rows[0].Kind, rows[1].Kind, rows[2].Kind})
}

func TestLoadSessionsRefusesAnAnswerItCannotTrust(t *testing.T) {
	for name, bad := range map[string]*ascv1.Session{
		"unknown kind":      {Id: "tok:1", Kind: ascv1.SessionKind_SESSION_KIND_UNSPECIFIED},
		"id of other kind":  {Id: "ssh:1", Kind: ascv1.SessionKind_SESSION_KIND_TOKEN},
		"id with a slash":   {Id: "tok:1/../x", Kind: ascv1.SessionKind_SESSION_KIND_TOKEN},
		"id without prefix": {Id: "7", Kind: ascv1.SessionKind_SESSION_KIND_TOKEN},
	} {
		t.Run(name, func(t *testing.T) {
			f, c := startAccountFake(t)
			f.list = func(*ascv1.ListSessionsRequest) (*ascv1.ListSessionsResponse, error) {
				return &ascv1.ListSessionsResponse{Sessions: []*ascv1.Session{bad}}, nil
			}
			_, err := LoadSessions(sysCtx(), c, "alice")
			assert.ErrorIs(t, err, ErrBadAnswer)
		})
	}
	t.Run("too many", func(t *testing.T) {
		f, c := startAccountFake(t)
		f.list = func(*ascv1.ListSessionsRequest) (*ascv1.ListSessionsResponse, error) {
			resp := &ascv1.ListSessionsResponse{}
			for i := 0; i <= MaxListItems; i++ {
				resp.Sessions = append(resp.Sessions, &ascv1.Session{Id: "tok:1", Kind: ascv1.SessionKind_SESSION_KIND_TOKEN})
			}
			return resp, nil
		}
		_, err := LoadSessions(sysCtx(), c, "alice")
		assert.ErrorIs(t, err, ErrBadAnswer)
	})
}

func TestRevokeSessionChecksBeforeTheCallAndCarriesTheOpID(t *testing.T) {
	f, c := startAccountFake(t)
	f.revoke = func(*ascv1.RevokeSessionRequest) (*ascv1.RevokeSessionResponse, error) {
		return &ascv1.RevokeSessionResponse{}, nil
	}
	op := uuid.Must(uuid.NewV7()).String()

	assert.ErrorIs(t, RevokeSession(sysCtx(), c, "alice", "../../etc", op), ErrBadInput)
	assert.ErrorIs(t, RevokeSession(sysCtx(), c, "alice", "tok:1", "not-a-uuid"), ErrBadInput)
	assert.Empty(t, f.reqs, "nothing was sent for a bad input")

	require.NoError(t, RevokeSession(sysCtx(), c, "alice", "tok:12", op))
	require.Len(t, f.reqs, 1)
	req := f.reqs[0].(*ascv1.RevokeSessionRequest)
	assert.Equal(t, "alice", req.GetLogin())
	assert.Equal(t, "tok:12", req.GetId())
	assert.Equal(t, op, f.ops[0])
}

func TestRevokeOtherSessions(t *testing.T) {
	f, c := startAccountFake(t)
	f.others = func(r *ascv1.RevokeOtherSessionsRequest) (*ascv1.RevokeOtherSessionsResponse, error) {
		return &ascv1.RevokeOtherSessionsResponse{Revoked: 4}, nil
	}
	op := uuid.Must(uuid.NewV7()).String()
	n, err := RevokeOtherSessions(sysCtx(), c, "alice", "tok:5", op)
	require.NoError(t, err)
	assert.Equal(t, uint32(4), n)
	assert.Equal(t, "tok:5", f.reqs[0].(*ascv1.RevokeOtherSessionsRequest).GetKeepId())
	_, err = RevokeOtherSessions(sysCtx(), c, "alice", "bogus", op)
	assert.ErrorIs(t, err, ErrBadInput)
}

func exportJob(id string, state ascv1.ExportState) *ascv1.ExportJob {
	return &ascv1.ExportJob{Id: id, State: state, Requested: 1700000000, Expires: 1800000000, SizeBytes: 2048, Rows: 12}
}

const goodExportID = "ex_abcdefghij0123456789"

func TestExports(t *testing.T) {
	f, c := startAccountFake(t)
	f.exports = func() (*ascv1.ListExportsResponse, error) {
		return &ascv1.ListExportsResponse{Jobs: []*ascv1.ExportJob{
			exportJob(goodExportID, ascv1.ExportState_EXPORT_STATE_READY),
			{Id: "ex_ABCDEFGHIJ0123456789", State: ascv1.ExportState_EXPORT_STATE_FAILED, Error: "disk\nfull‮"},
			{Id: "ex_zzzzzzzzzzzzzzzzzzzz", State: ascv1.ExportState_EXPORT_STATE_RUNNING},
		}}, nil
	}
	rows, err := ListExports(sysCtx(), c)
	require.NoError(t, err)
	require.Len(t, rows, 3)
	now := time.Unix(1750000000, 0)
	assert.True(t, rows[0].Downloadable(now))
	assert.False(t, rows[0].Downloadable(time.Unix(1900000000, 0)), "an expired archive is not offered")
	assert.False(t, rows[1].Downloadable(now))
	assert.Equal(t, "disk full", rows[1].Error)
	assert.True(t, rows[2].Waiting())
	assert.Equal(t, int64(2048), rows[0].Size)

	f.exports = func() (*ascv1.ListExportsResponse, error) {
		return &ascv1.ListExportsResponse{Jobs: []*ascv1.ExportJob{exportJob("ex_short", ascv1.ExportState_EXPORT_STATE_READY)}}, nil
	}
	_, err = ListExports(sysCtx(), c)
	assert.ErrorIs(t, err, ErrBadAnswer, "an id of the wrong shape is refused")
}

func TestRequestAndDeleteExport(t *testing.T) {
	f, c := startAccountFake(t)
	f.request = func() (*ascv1.RequestExportResponse, error) {
		return &ascv1.RequestExportResponse{Job: exportJob(goodExportID, ascv1.ExportState_EXPORT_STATE_PENDING)}, nil
	}
	f.del = func(*ascv1.DeleteExportRequest) (*ascv1.DeleteExportResponse, error) {
		return &ascv1.DeleteExportResponse{}, nil
	}
	op := uuid.Must(uuid.NewV7()).String()
	row, err := RequestExport(sysCtx(), c, op)
	require.NoError(t, err)
	assert.Equal(t, "pending", row.State)
	assert.Equal(t, op, f.ops[0])
	_, err = RequestExport(sysCtx(), c, "nope")
	assert.ErrorIs(t, err, ErrBadInput)

	require.NoError(t, DeleteExport(sysCtx(), c, goodExportID, op))
	assert.ErrorIs(t, DeleteExport(sysCtx(), c, "ex_x", op), ErrBadInput)
}

func TestCopyDataExportStreamsInOrder(t *testing.T) {
	f, c := startAccountFake(t)
	f.download = func(r *ascv1.DownloadExportRequest, s grpc.ServerStreamingServer[ascv1.ExportChunk]) error {
		if err := s.Send(&ascv1.ExportChunk{Offset: 0, Data: []byte("hello ")}); err != nil {
			return err
		}
		return s.Send(&ascv1.ExportChunk{Offset: 6, Data: []byte("world"), Last: true})
	}
	var buf bytes.Buffer
	n, err := CopyDataExport(sysCtx(), c, goodExportID, &buf)
	require.NoError(t, err)
	assert.Equal(t, int64(11), n)
	assert.Equal(t, "hello world", buf.String())

	_, err = CopyDataExport(sysCtx(), c, "ex_../x", &buf)
	assert.ErrorIs(t, err, ErrBadInput)
}

func TestCopyDataExportRefusesAGapAndATruncatedStream(t *testing.T) {
	f, c := startAccountFake(t)
	f.download = func(r *ascv1.DownloadExportRequest, s grpc.ServerStreamingServer[ascv1.ExportChunk]) error {
		return s.Send(&ascv1.ExportChunk{Offset: 0, Data: []byte("abc")})
	}
	var buf bytes.Buffer
	_, err := CopyDataExport(sysCtx(), c, goodExportID, &buf)
	assert.ErrorIs(t, err, ErrBadAnswer, "a stream with no last chunk is incomplete")

	f.download = func(r *ascv1.DownloadExportRequest, s grpc.ServerStreamingServer[ascv1.ExportChunk]) error {
		if err := s.Send(&ascv1.ExportChunk{Offset: 0, Data: []byte("abc")}); err != nil {
			return err
		}
		return s.Send(&ascv1.ExportChunk{Offset: 99, Data: []byte("def"), Last: true})
	}
	buf.Reset()
	_, err = CopyDataExport(sysCtx(), c, goodExportID, &buf)
	assert.ErrorIs(t, err, ErrBadAnswer, "a gap is refused")
	assert.False(t, strings.Contains(buf.String(), "def"))
}

func sysCtx() context.Context { return ascclient.WithSystem(context.Background()) }
