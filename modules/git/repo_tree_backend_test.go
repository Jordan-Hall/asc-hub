// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package git

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// treeBackend is a Backend that knows one commit, its root tree and one subtree, and nothing else. Like the asc
// backend it lists a tree only after it has learned where the tree is, and it learns that from the commit and from
// the listing of the parent: a tree found by another way cannot be listed.
type treeBackend struct {
	Backend
	known map[string]bool
	root  ObjectID
	sub   ObjectID
	head  ObjectID
}

func (b *treeBackend) StatObjects(_ context.Context, ids []ObjectID) ([]ObjectInfo, error) {
	out := make([]ObjectInfo, len(ids))
	for i, id := range ids {
		switch id.String() {
		case b.head.String():
			out[i] = ObjectInfo{ID: id, Type: ObjectCommit}
		case b.root.String(), b.sub.String():
			out[i] = ObjectInfo{ID: id, Type: ObjectTree}
		default:
			out[i] = ObjectInfo{ID: id, Missing: true}
		}
	}
	return out, nil
}

func (b *treeBackend) ReadCommit(_ context.Context, id ObjectID) (*CommitData, error) {
	if id.String() != b.head.String() {
		return nil, ErrNotExist{ID: id.String()}
	}
	b.known[b.root.String()] = true
	return &CommitData{ID: id, Tree: b.root}, nil
}

func (b *treeBackend) ReadTree(_ context.Context, id ObjectID) ([]TreeEntryData, error) {
	if !b.known[id.String()] {
		return nil, ErrNotExist{ID: id.String()}
	}
	if id.String() == b.root.String() {
		b.known[b.sub.String()] = true
		return []TreeEntryData{{ID: b.sub, Name: "dir", Mode: EntryModeTree}}, nil
	}
	return []TreeEntryData{{ID: Sha1ObjectFormat.EmptyTree(), Name: "leaf", Mode: EntryModeTree}}, nil
}

func TestGetTreeThroughANonGitBackend(t *testing.T) {
	mk := func(s string) ObjectID {
		id, err := NewIDFromString(s)
		require.NoError(t, err)
		return id
	}
	be := &treeBackend{
		known: map[string]bool{},
		head:  mk("1111111111111111111111111111111111111111"),
		root:  mk("2222222222222222222222222222222222222222"),
		sub:   mk("3333333333333333333333333333333333333333"),
	}
	repo := &Repository{Ctx: t.Context(), backend: be, backendName: "asc"}

	// A commit id gives its root tree, and teaches the backend where it is.
	root, err := repo.getTree(be.head)
	require.NoError(t, err)
	assert.Equal(t, be.root.String(), root.ID.String())
	entries, err := root.ListEntries()
	require.NoError(t, err)
	require.Len(t, entries, 1)

	// A subtree found in a listing of its parent can be listed.
	sub, err := repo.getTree(entries[0].ID)
	require.NoError(t, err)
	got, err := sub.ListEntries()
	require.NoError(t, err)
	assert.Len(t, got, 1)

	// An id the backend does not know is not a tree.
	_, err = repo.getTree(mk("4444444444444444444444444444444444444444"))
	assert.True(t, IsErrNotExist(err), "%v", err)
}
