// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package asc_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"

	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/git"
	"forgejo.org/modules/repostore"
	"forgejo.org/modules/repostore/asc"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// gid is a git commit id made of one repeated byte.
func gid(c byte) git.ObjectID {
	return git.Sha1ObjectFormat.MustID(bytes.Repeat([]byte{c}, 20))
}

// ascid is the changeset id the fake maps gid(c) to: the git id followed by a fixed tail.
func ascid(c byte) []byte {
	return append(bytes.Repeat([]byte{c}, 20), bytes.Repeat([]byte{0xaa}, 12)...)
}

func hexOf(c byte) string { return fmt.Sprintf("%x", ascid(c)) }

// fake implements the part of ascv1.RepoClient the Writer uses. A call it does not implement panics on the
// embedded nil interface, which fails the test that made it.
type fake struct {
	// noCountOnly makes Log refuse count_only, as an asc server without it does.
	noCountOnly bool
	ascv1.RepoClient

	mu      sync.Mutex
	calls   []string
	reqs    map[string][]any
	unknown map[string]bool // git ids (hex) that GitMap does not know
	refs    map[string]git.ObjectID

	commitID   byte // git id of the next Commit/Apply/Merge result (0 = 0x70)
	commitErr  error
	mergeResp  *ascv1.MergeResponse
	mergeErr   error
	applyResp  *ascv1.ApplyResponse
	applyErr   error
	updateErr  error
	adminResp  *ascv1.RepoAdminResponse
	adminErr   error
	logEntries map[string][]*ascv1.LogEntry // "include|exclude" -> entries
	logCount   map[string]uint64
	mergeBase  map[string]git.ObjectID // "a|b" (git hex) -> base
	diffText   map[ascv1.DiffFormat]string
	header     *ascv1.LogEntry // ReadChangeset header
}

func newFake() *fake {
	return &fake{
		reqs:       map[string][]any{},
		unknown:    map[string]bool{},
		refs:       map[string]git.ObjectID{},
		logEntries: map[string][]*ascv1.LogEntry{},
		logCount:   map[string]uint64{},
		mergeBase:  map[string]git.ObjectID{},
		diffText:   map[ascv1.DiffFormat]string{},
	}
}

func (f *fake) rec(name string, req any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, name)
	f.reqs[name] = append(f.reqs[name], req)
}

func (f *fake) last(name string) any {
	f.mu.Lock()
	defer f.mu.Unlock()
	r := f.reqs[name]
	if len(r) == 0 {
		return nil
	}
	return r[len(r)-1]
}

func (f *fake) called(name string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.reqs[name])
}

func (f *fake) result() byte {
	if f.commitID == 0 {
		return 0x70
	}
	return f.commitID
}

func (f *fake) GitMap(_ context.Context, in *ascv1.GitMapRequest, _ ...grpc.CallOption) (*ascv1.GitMapResponse, error) {
	f.rec("GitMap", in)
	out := &ascv1.GitMapResponse{}
	for _, g := range in.GetGit() {
		if f.unknown[fmt.Sprintf("%x", g)] {
			out.GitToAsc = append(out.GitToAsc, nil)
			continue
		}
		out.GitToAsc = append(out.GitToAsc, append(bytes.Clone(g), bytes.Repeat([]byte{0xaa}, 12)...))
	}
	for _, c := range in.GetAsc() {
		out.AscToGit = append(out.AscToGit, bytes.Clone(c[:20]))
	}
	return out, nil
}

func (f *fake) Resolve(_ context.Context, in *ascv1.ResolveRequest, _ ...grpc.CallOption) (*ascv1.ResolveResponse, error) {
	f.rec("Resolve", in)
	out := &ascv1.ResolveResponse{}
	for _, rev := range in.GetRevspecs() {
		if id, ok := f.refs[in.GetRepo().GetName()+"|"+rev]; ok {
			out.Ids = append(out.Ids, id.RawValue())
		} else {
			out.Ids = append(out.Ids, nil)
		}
	}
	return out, nil
}

func (f *fake) CopyClosure(_ context.Context, in *ascv1.CopyClosureRequest, _ ...grpc.CallOption) (*ascv1.CopyClosureResponse, error) {
	f.rec("CopyClosure", in)
	return &ascv1.CopyClosureResponse{Copied: 1}, nil
}

func (f *fake) Commit(_ context.Context, in *ascv1.CommitRequest, _ ...grpc.CallOption) (*ascv1.CommitResponse, error) {
	f.rec("Commit", in)
	if f.commitErr != nil {
		return nil, f.commitErr
	}
	return &ascv1.CommitResponse{Id: ascid(f.result()), Git: gid(f.result()).RawValue()}, nil
}

func (f *fake) Apply(_ context.Context, in *ascv1.ApplyRequest, _ ...grpc.CallOption) (*ascv1.ApplyResponse, error) {
	f.rec("Apply", in)
	if f.applyErr != nil {
		return nil, f.applyErr
	}
	if f.applyResp != nil {
		return f.applyResp, nil
	}
	return &ascv1.ApplyResponse{Result: ascid(f.result())}, nil
}

func (f *fake) Merge(_ context.Context, in *ascv1.MergeRequest, _ ...grpc.CallOption) (*ascv1.MergeResponse, error) {
	f.rec("Merge", in)
	if f.mergeErr != nil {
		return nil, f.mergeErr
	}
	if f.mergeResp != nil {
		return f.mergeResp, nil
	}
	return &ascv1.MergeResponse{Result: ascid(f.result())}, nil
}

func (f *fake) UpdateRefs(_ context.Context, in *ascv1.UpdateRefsRequest, _ ...grpc.CallOption) (*ascv1.UpdateRefsResponse, error) {
	f.rec("UpdateRefs", in)
	if f.updateErr != nil {
		return nil, f.updateErr
	}
	out := &ascv1.UpdateRefsResponse{}
	for _, u := range in.GetUpdates() {
		out.Results = append(out.Results, &ascv1.RefResult{Name: u.GetName(), New: u.GetNew()})
		if !u.GetDelete() {
			f.refs[in.GetRepo().GetName()+"|"+u.GetName()] = git.Sha1ObjectFormat.MustID(u.GetNew().GetChangeset()[:20])
		} else {
			delete(f.refs, in.GetRepo().GetName()+"|"+u.GetName())
		}
	}
	return out, nil
}

func (f *fake) ReadChangeset(_ context.Context, in *ascv1.ReadChangesetRequest, _ ...grpc.CallOption) (*ascv1.ReadChangesetResponse, error) {
	f.rec("ReadChangeset", in)
	return &ascv1.ReadChangesetResponse{RootGit: gid(0x55).RawValue(), Header: f.header}, nil
}

func (f *fake) MergeBase(_ context.Context, in *ascv1.MergeBaseRequest, _ ...grpc.CallOption) (*ascv1.MergeBaseResponse, error) {
	f.rec("MergeBase", in)
	key := fmt.Sprintf("%x|%x", in.GetA(), in.GetB()[0])
	if id, ok := f.mergeBase[key]; ok {
		return &ascv1.MergeBaseResponse{Bases: [][]byte{id.RawValue()}}, nil
	}
	return &ascv1.MergeBaseResponse{}, nil
}

func (f *fake) RepoAdmin(_ context.Context, in *ascv1.RepoAdminRequest, _ ...grpc.CallOption) (*ascv1.RepoAdminResponse, error) {
	f.rec("RepoAdmin", in)
	if f.adminErr != nil {
		return nil, f.adminErr
	}
	if f.adminResp != nil {
		return f.adminResp, nil
	}
	return &ascv1.RepoAdminResponse{}, nil
}

type stream[T any] struct {
	grpc.ClientStream
	items []*T
}

func (s *stream[T]) Recv() (*T, error) {
	if len(s.items) == 0 {
		return nil, io.EOF
	}
	it := s.items[0]
	s.items = s.items[1:]
	return it, nil
}

func (f *fake) Log(_ context.Context, in *ascv1.LogRequest, _ ...grpc.CallOption) (grpc.ServerStreamingClient[ascv1.LogResponse], error) {
	f.rec("Log", in)
	if f.noCountOnly && in.GetCountOnly() {
		return nil, status.Error(codes.Unimplemented, "count_only, pickaxe and resume_token are not available on this server yet")
	}
	key := strings.Join(in.GetInclude(), ",") + "|" + strings.Join(in.GetExclude(), ",")
	return &stream[ascv1.LogResponse]{items: []*ascv1.LogResponse{{Entries: f.logEntries[key], Count: f.logCount[key]}}}, nil
}

func (f *fake) Diff(_ context.Context, in *ascv1.DiffRequest, _ ...grpc.CallOption) (grpc.ServerStreamingClient[ascv1.DiffResponse], error) {
	f.rec("Diff", in)
	return &stream[ascv1.DiffResponse]{items: []*ascv1.DiffResponse{{Text: []byte(f.diffText[in.GetFormat()])}}}, nil
}

// failure builds a gRPC error that carries an ErrorDetail, as asc serve sends.
func failure(code codes.Code, detail, msg string, paths ...string) error {
	st := status.New(code, msg)
	if detail != "" {
		withDetail, err := st.WithDetails(&ascv1.ErrorDetail{Code: detail, Paths: paths})
		if err == nil {
			st = withDetail
		}
	}
	return st.Err()
}

// ref returns a RepoRef of owner/name.
func ref(owner, name string) repostore.RepoRef {
	return repostore.RepoRef{ID: 1, Owner: owner, Name: name, ObjectFormat: "sha1", StorageBackend: "asc", Path: "/repos/" + owner + "/" + name + ".git"}
}

func newWriter(t *testing.T, f *fake) *asc.Writer {
	t.Helper()
	w, err := asc.New(f, asc.Options{Auth: func(ctx context.Context) (context.Context, error) { return ctx, nil }})
	require.NoError(t, err)
	return w
}

func sig(name string) *repostore.Signature {
	return &repostore.Signature{Name: name, Email: strings.ToLower(name) + "@example.com"}
}
