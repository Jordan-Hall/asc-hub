// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package repostore_test

import (
	"context"
	"sync"
	"testing"

	"forgejo.org/modules/repostore"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stub answers Size with its own id, so a test can tell which Writer served a request.
type stub struct {
	repostore.Writer
	id int64
}

func (s stub) Size(context.Context, repostore.RepoRef) (repostore.Size, error) {
	return repostore.Size{Logical: s.id}, nil
}

func (s stub) Init(context.Context, repostore.InitRequest) error { return nil }

func (s stub) Merge(context.Context, repostore.MergeRequest) (*repostore.MergeResult, error) {
	return &repostore.MergeResult{Ahead: int(s.id)}, nil
}

var registerOnce sync.Once

func registerStubs() {
	registerOnce.Do(func() {
		repostore.Register(repostore.DefaultName, stub{id: 1})
		repostore.Register(repostore.AscName, stub{id: 2})
	})
}

func TestDefaultRoutesByStorageBackend(t *testing.T) {
	registerStubs()
	repostore.UseAscWriter(false)
	w, err := repostore.Default()
	require.NoError(t, err)
	size, err := w.Size(t.Context(), repostore.RepoRef{Owner: "o", Name: "r", StorageBackend: "asc"})
	require.NoError(t, err)
	assert.EqualValues(t, 1, size.Logical, "with the asc writer off, an asc repository is written like a git one")

	repostore.UseAscWriter(true)
	t.Cleanup(func() { repostore.UseAscWriter(false) })
	w, err = repostore.Default()
	require.NoError(t, err)

	for backend, want := range map[string]int64{"": 1, "git": 1, "dual": 1, "asc": 2} {
		size, err := w.Size(t.Context(), repostore.RepoRef{Owner: "o", Name: "r", StorageBackend: backend})
		require.NoError(t, err)
		assert.Equal(t, want, size.Logical, "storage backend %q", backend)
	}
}

func TestDefaultRefusesARequestThatSpansBackends(t *testing.T) {
	registerStubs()
	repostore.UseAscWriter(true)
	t.Cleanup(func() { repostore.UseAscWriter(false) })
	w, err := repostore.Default()
	require.NoError(t, err)

	asc := repostore.RepoRef{Owner: "o", Name: "r", StorageBackend: "asc"}
	plain := repostore.RepoRef{Owner: "o", Name: "fork"}
	err = w.Fork(t.Context(), repostore.ForkRequest{Repo: plain, From: asc})
	require.ErrorIs(t, err, repostore.ErrUnsupported)
	_, err = w.Merge(t.Context(), repostore.MergeRequest{Base: asc, Head: plain})
	require.ErrorIs(t, err, repostore.ErrUnsupported)

	res, err := w.Merge(t.Context(), repostore.MergeRequest{Base: asc, Head: asc, Publish: &repostore.RefUpdate{Repo: asc}})
	require.NoError(t, err)
	assert.Equal(t, 2, res.Ahead)
	require.NoError(t, w.Init(t.Context(), repostore.InitRequest{Repo: asc}))
}
