// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package ascbackend_test

import (
	"bytes"
	"context"
	"net"
	"testing"

	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/git/ascbackend"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
)

type classifyServer struct {
	ascv1.UnimplementedRepoServer
	resp *ascv1.ClassifyResponse
	err  error
	req  *ascv1.ClassifyRequest
}

func (s *classifyServer) Classify(_ context.Context, req *ascv1.ClassifyRequest) (*ascv1.ClassifyResponse, error) {
	s.req = req
	return s.resp, s.err
}

func newClassifyBackend(t *testing.T, srv *classifyServer) *ascbackend.Backend {
	t.Helper()
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	gs := grpc.NewServer()
	ascv1.RegisterRepoServer(gs, srv)
	go func() { _ = gs.Serve(lis) }()
	t.Cleanup(gs.Stop)
	conn, err := grpc.NewClient(lis.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	b, err := ascbackend.New(ascv1.NewRepoClient(conn), fakeRepoName, nil, ascbackend.Options{})
	require.NoError(t, err)
	return b
}

func fileClass(path string, c ascv1.ChangeClass) *ascv1.FileClass {
	return &ascv1.FileClass{Path: []byte(path), Class: c}
}

func TestClassify(t *testing.T) {
	const (
		docs  = ascv1.ChangeClass_CHANGE_CLASS_DOCS
		lock  = ascv1.ChangeClass_CHANGE_CLASS_LOCKFILE
		logic = ascv1.ChangeClass_CHANGE_CLASS_LOGIC
	)
	t.Run("a good answer is returned and the request is built from the arguments", func(t *testing.T) {
		srv := &classifyServer{resp: &ascv1.ClassifyResponse{
			Files:   []*ascv1.FileClass{fileClass("a.md", docs), fileClass("Cargo.lock", lock)},
			Overall: lock,
		}}
		b := newClassifyBackend(t, srv)
		got, err := b.Classify(t.Context(), "refs/heads/main", "refs/pull/1/head", true)
		require.NoError(t, err)
		assert.Equal(t, lock, got.Overall)
		assert.Equal(t, []ascbackend.FileClass{{Path: "a.md", Class: docs}, {Path: "Cargo.lock", Class: lock}}, got.Files)
		assert.Equal(t, "refs/heads/main", srv.req.GetBase())
		assert.Equal(t, "refs/pull/1/head", srv.req.GetHead())
		assert.True(t, srv.req.GetMergeBase())
		assert.Equal(t, fakeRepoName, srv.req.GetRepo().GetName())
		assert.Equal(t, ascv1.IdMode_ID_MODE_GIT, srv.req.GetRepo().GetIdMode())
	})

	bad := map[string]*ascv1.ClassifyResponse{
		"unknown overall":        {Files: []*ascv1.FileClass{fileClass("a", docs)}, Overall: 42},
		"unknown file class":     {Files: []*ascv1.FileClass{fileClass("a", 42)}, Overall: logic},
		"overall below the max":  {Files: []*ascv1.FileClass{fileClass("a", docs), fileClass("b", logic)}, Overall: docs},
		"overall above the max":  {Files: []*ascv1.FileClass{fileClass("a", docs)}, Overall: logic},
		"empty path":             {Files: []*ascv1.FileClass{fileClass("", docs)}, Overall: docs},
		"path with a NUL":        {Files: []*ascv1.FileClass{fileClass("a\x00b", docs)}, Overall: docs},
		"path over the limit":    {Files: []*ascv1.FileClass{{Path: bytes.Repeat([]byte("a"), 4097), Class: docs}}, Overall: docs},
		"path listed twice":      {Files: []*ascv1.FileClass{fileClass("a", docs), fileClass("a", docs)}, Overall: docs},
		"too many files (limit)": {Files: manyFiles(100_001), Overall: docs},
	}
	for name, resp := range bad {
		t.Run("refuses "+name, func(t *testing.T) {
			b := newClassifyBackend(t, &classifyServer{resp: resp})
			got, err := b.Classify(t.Context(), "a", "b", false)
			require.Error(t, err)
			assert.Nil(t, got)
		})
	}

	t.Run("refuses a bad revision before calling", func(t *testing.T) {
		srv := &classifyServer{resp: &ascv1.ClassifyResponse{}}
		b := newClassifyBackend(t, srv)
		_, err := b.Classify(t.Context(), "", "b", false)
		require.Error(t, err)
		_, err = b.Classify(t.Context(), "a", "b\nc", false)
		require.Error(t, err)
		assert.Nil(t, srv.req)
	})

	t.Run("a server error is an error", func(t *testing.T) {
		b := newClassifyBackend(t, &classifyServer{err: status.Error(codes.Internal, "boom")})
		_, err := b.Classify(t.Context(), "a", "b", false)
		require.Error(t, err)
	})
}

func manyFiles(n int) []*ascv1.FileClass {
	files := make([]*ascv1.FileClass, n)
	for i := range files {
		files[i] = &ascv1.FileClass{Path: []byte("f" + string(rune('a'+i%26)) + string(rune('a'+i/26%26)) + string(rune('a'+i/676%26)) + string(rune('a'+i/17576%26))), Class: ascv1.ChangeClass_CHANGE_CLASS_DOCS}
	}
	return files
}

func TestClassifyRepositoryNeedsAClient(t *testing.T) {
	ascbackend.Reset()
	_, err := ascbackend.ClassifyRepository(t.Context(), "/data/repos/o/r.git", "a", "b", true)
	require.ErrorIs(t, err, ascbackend.ErrNotConfigured)
}
