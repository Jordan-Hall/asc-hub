// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package ascbackend_test

import (
	"bytes"
	"context"
	"encoding/hex"
	"testing"

	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/git"
	"forgejo.org/modules/git/ascbackend"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
)

// gitMapClient answers GitMap only. Every other call panics on the nil embedded client.
type gitMapClient struct {
	ascv1.RepoClient
	answer func(req *ascv1.GitMapRequest) *ascv1.GitMapResponse
	calls  int
}

func (c *gitMapClient) GitMap(_ context.Context, req *ascv1.GitMapRequest, _ ...grpc.CallOption) (*ascv1.GitMapResponse, error) {
	c.calls++
	return c.answer(req), nil
}

func TestChangesetIDs(t *testing.T) {
	a := git.MustIDFromString("1111111111111111111111111111111111111111")
	b := git.MustIDFromString("2222222222222222222222222222222222222222")
	ascA, ascB := bytes.Repeat([]byte{0xaa}, 32), bytes.Repeat([]byte{0xbb}, 32)

	open := func(t *testing.T, client *gitMapClient) *ascbackend.Backend {
		be, err := ascbackend.New(client, "owner/repo", nil, ascbackend.Options{
			Auth: func(ctx context.Context) (context.Context, error) { return ctx, nil },
		})
		require.NoError(t, err)
		return be
	}

	t.Run("maps in order and asks for git ids", func(t *testing.T) {
		client := &gitMapClient{answer: func(req *ascv1.GitMapRequest) *ascv1.GitMapResponse {
			assert.Empty(t, req.GetAsc())
			require.Len(t, req.GetGit(), 2)
			assert.Equal(t, a.RawValue(), req.GetGit()[0])
			return &ascv1.GitMapResponse{GitToAsc: [][]byte{ascA, ascB}}
		}}
		got, err := open(t, client).ChangesetIDs(t.Context(), []git.ObjectID{a, b})
		require.NoError(t, err)
		assert.Equal(t, []string{hex.EncodeToString(ascA), hex.EncodeToString(ascB)}, got)
	})

	t.Run("a commit without a changeset fails the call", func(t *testing.T) {
		client := &gitMapClient{answer: func(*ascv1.GitMapRequest) *ascv1.GitMapResponse {
			return &ascv1.GitMapResponse{GitToAsc: [][]byte{ascA, nil}}
		}}
		_, err := open(t, client).ChangesetIDs(t.Context(), []git.ObjectID{a, b})
		require.ErrorIs(t, err, git.ErrNotExist{ID: b.String()})
	})

	t.Run("an answer that fails verification is refused", func(t *testing.T) {
		for name, resp := range map[string]*ascv1.GitMapResponse{
			"too few ids":        {GitToAsc: [][]byte{ascA}},
			"too many ids":       {GitToAsc: [][]byte{ascA, ascB, ascB}},
			"a short id":         {GitToAsc: [][]byte{ascA, ascB[:31]}},
			"a long id":          {GitToAsc: [][]byte{ascA, append(bytes.Clone(ascB), 0)}},
			"no ids":             {},
			"only the wrong map": {AscToGit: [][]byte{ascA, ascB}},
		} {
			t.Run(name, func(t *testing.T) {
				client := &gitMapClient{answer: func(*ascv1.GitMapRequest) *ascv1.GitMapResponse { return resp }}
				_, err := open(t, client).ChangesetIDs(t.Context(), []git.ObjectID{a, b})
				require.Error(t, err)
			})
		}
	})

	t.Run("an id of another object format is refused before any call", func(t *testing.T) {
		client := &gitMapClient{answer: func(*ascv1.GitMapRequest) *ascv1.GitMapResponse { return &ascv1.GitMapResponse{} }}
		sha256ID := git.MustIDFromString("1111111111111111111111111111111111111111111111111111111111111111")
		_, err := open(t, client).ChangesetIDs(t.Context(), []git.ObjectID{sha256ID})
		require.Error(t, err)
		assert.Zero(t, client.calls)
	})

	t.Run("an empty list makes no call", func(t *testing.T) {
		client := &gitMapClient{answer: func(*ascv1.GitMapRequest) *ascv1.GitMapResponse { return &ascv1.GitMapResponse{} }}
		got, err := open(t, client).ChangesetIDs(t.Context(), nil)
		require.NoError(t, err)
		assert.Empty(t, got)
	})
}
