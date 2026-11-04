// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package ascbridge

import (
	"context"
	"encoding/hex"
	"io"
	"strings"
	"testing"

	ascv1 "forgejo.org/modules/ascproto/ascv1"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestRestackStackSendsResolutionsAndReadsConflictFiles(t *testing.T) {
	ops, stacks, _ := newOps(t)
	stacks.restackResp = &ascv1.RestackStackResponse{
		Onto: id32(0xaa),
		Items: []*ascv1.RestackItemResult{
			{Change: mustChange(t, changeB), State: ascv1.RestackItemState_RESTACK_ITEM_STATE_CONFLICT, OldHead: id32(0x33),
				Conflicts: []*ascv1.Conflict{
					{Path: []byte("docs/a.md"), Kind: ascv1.ConflictKind_CONFLICT_KIND_CONTENT, Changeset: id32(0x33),
						BaseId: id32(1), OursId: id32(2), TheirsId: id32(3), MarkedId: id32(4)},
					{Path: []byte("img.png"), Kind: ascv1.ConflictKind_CONFLICT_KIND_BINARY, Changeset: id32(0x33), OursId: id32(5)},
					// Left out of Files: no change, a bad id, an unknown kind.
					{Path: []byte("x"), Kind: ascv1.ConflictKind_CONFLICT_KIND_CONTENT},
					{Path: []byte("y"), Kind: ascv1.ConflictKind_CONFLICT_KIND_CONTENT, Changeset: id32(0x33), MarkedId: []byte{1}},
					{Path: []byte("z"), Kind: ascv1.ConflictKind(99), Changeset: id32(0x33)},
				}},
		},
	}
	res, err := ops.RestackStack(t.Context(), RestackStackRequest{Repo: "o/r", Change: changeB, Resolutions: []Resolution{
		{ChangesetID: hex.EncodeToString(id32(0x33)), Path: "docs/a.md", Content: []byte("merged\n")},
		{ChangesetID: hex.EncodeToString(id32(0x33)), Path: "old.txt", Delete: true},
	}})
	require.NoError(t, err)
	got := stacks.restackReq[0].GetResolutions()
	require.Len(t, got, 2)
	assert.Equal(t, id32(0x33), got[0].GetChangeset())
	assert.Equal(t, []byte("docs/a.md"), got[0].GetPath())
	assert.Equal(t, []byte("merged\n"), got[0].GetOp().GetPutContent())
	assert.True(t, got[1].GetOp().GetDelete())

	files := res.Items[0].Files
	require.Len(t, files, 2)
	assert.Equal(t, ConflictFile{
		Path: "docs/a.md", Kind: "content", ChangesetID: hex.EncodeToString(id32(0x33)),
		BaseID: hex.EncodeToString(id32(1)), OursID: hex.EncodeToString(id32(2)), TheirsID: hex.EncodeToString(id32(3)),
		MarkedID: hex.EncodeToString(id32(4)),
	}, files[0])
	assert.Equal(t, "binary", files[1].Kind)
	assert.Empty(t, files[1].MarkedID)
	assert.Len(t, res.Items[0].Conflicts, 5, "the paths still name every conflict")
}

func TestResolutionsAreChecked(t *testing.T) {
	cs := hex.EncodeToString(id32(0x33))
	for name, r := range map[string][]Resolution{
		"no changeset":  {{Path: "a"}},
		"short id":      {{ChangesetID: "abcd", Path: "a"}},
		"no path":       {{ChangesetID: cs}},
		"NUL in path":   {{ChangesetID: cs, Path: "a\x00b"}},
		"invalid UTF-8": {{ChangesetID: cs, Path: "a\xff"}},
		"too much text": {{ChangesetID: cs, Path: "a", Content: make([]byte, MaxResolutionBytes)}, {ChangesetID: cs, Path: "b", Content: []byte("x")}},
	} {
		ops, stacks, _ := newOps(t)
		_, err := ops.RestackStack(t.Context(), RestackStackRequest{Repo: "o/r", Change: changeB, Resolutions: r})
		require.ErrorIs(t, err, ErrStackBadRequest, name)
		assert.Empty(t, stacks.restackReq, "%s: asc is not asked", name)
	}
}

type fakeReadFile struct {
	ascv1.RepoClient

	req   *ascv1.ReadFileRequest
	parts []*ascv1.ReadFileResponse
	err   error
}

func (f *fakeReadFile) ReadFile(_ context.Context, in *ascv1.ReadFileRequest, _ ...grpc.CallOption) (grpc.ServerStreamingClient[ascv1.ReadFileResponse], error) {
	f.req = in
	if f.err != nil {
		return nil, f.err
	}
	return &fakeReadStream{parts: f.parts}, nil
}

type fakeReadStream struct {
	grpc.ClientStream
	parts []*ascv1.ReadFileResponse
}

func (s *fakeReadStream) Recv() (*ascv1.ReadFileResponse, error) {
	if len(s.parts) == 0 {
		return nil, io.EOF
	}
	m := s.parts[0]
	s.parts = s.parts[1:]
	return m, nil
}

func TestReadFileByID(t *testing.T) {
	id := hex.EncodeToString(id32(7))
	use := func(t *testing.T, f *fakeReadFile) {
		t.Cleanup(UseClient(f))
	}

	t.Run("reads the whole file", func(t *testing.T) {
		f := &fakeReadFile{parts: []*ascv1.ReadFileResponse{{TotalSize: 6, Data: []byte("abc")}, {Data: []byte("def")}}}
		use(t, f)
		data, truncated, err := ReadFileByID(t.Context(), "o/r", id, 10)
		require.NoError(t, err)
		assert.Equal(t, "abcdef", string(data))
		assert.False(t, truncated)
		assert.Equal(t, "o/r", f.req.GetRepo().GetName())
		assert.Equal(t, id32(7), f.req.GetFileId())
		assert.EqualValues(t, 11, f.req.GetLength(), "one byte more says whether the file is longer")
	})

	t.Run("a longer file is cut at the limit", func(t *testing.T) {
		use(t, &fakeReadFile{parts: []*ascv1.ReadFileResponse{{TotalSize: 100, Data: []byte("abcd")}}})
		data, truncated, err := ReadFileByID(t.Context(), "o/r", id, 3)
		require.NoError(t, err)
		assert.Equal(t, "abc", string(data))
		assert.True(t, truncated)
	})

	t.Run("bad answers", func(t *testing.T) {
		for name, parts := range map[string][]*ascv1.ReadFileResponse{
			"nothing":            nil,
			"more than asked":    {{TotalSize: 100, Data: []byte(strings.Repeat("x", 5))}},
			"more than the file": {{TotalSize: 1, Data: []byte("ab")}},
		} {
			use(t, &fakeReadFile{parts: parts})
			_, _, err := ReadFileByID(t.Context(), "o/r", id, 3)
			assert.ErrorIs(t, err, ErrBadAnswer, name)
		}
	})

	t.Run("bad ids and limits are refused before asc", func(t *testing.T) {
		f := &fakeReadFile{}
		use(t, f)
		for _, bad := range []string{"", "zz", hex.EncodeToString(id32(1)[:31])} {
			_, _, err := ReadFileByID(t.Context(), "o/r", bad, 3)
			assert.ErrorIs(t, err, ErrStackBadRequest)
		}
		_, _, err := ReadFileByID(t.Context(), "o/r", id, 0)
		assert.ErrorIs(t, err, ErrStackBadRequest)
		assert.Nil(t, f.req)
	})

	t.Run("errors of asc do not leak its message", func(t *testing.T) {
		use(t, &fakeReadFile{err: status.Error(codes.NotFound, "secret detail")})
		_, _, err := ReadFileByID(t.Context(), "o/r", id, 3)
		require.Error(t, err)
		assert.NotContains(t, err.Error(), "secret detail")
	})
}
