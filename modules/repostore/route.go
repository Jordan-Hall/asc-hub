// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package repostore

import (
	"context"
	"fmt"
	"sync/atomic"
)

// AscName names the Writer that serves repositories with storage_backend=asc (modules/repostore/asc, K12).
const AscName = "asc"

// StorageBackendAsc is the RepoRef.StorageBackend of a repository stored in asc.
const StorageBackendAsc = "asc"

var ascWriterOn atomic.Bool

// UseAscWriter turns the routing of storage_backend=asc repositories to the asc Writer on or off ([asc] WRITER = asc,
// Stage 4 of SPEC 13.1). While it is off, an asc repository is written like a git one: by temp-repository pushes
// into its git bridge, which asc imports (Stage 2 and 3, K09).
func UseAscWriter(on bool) { ascWriterOn.Store(on) }

// AscWriterEnabled reports whether storage_backend=asc repositories are written through the asc Writer.
func AscWriterEnabled() bool { return ascWriterOn.Load() }

// router sends each request to the Writer of the repository's storage backend.
type router struct {
	git Writer
}

var _ Writer = (*router)(nil)

// pick returns the Writer for refs. Refs of different kinds in one request are refused: no Writer can make
// a change that spans a git directory and an asc repository.
func (r *router) pick(refs ...RepoRef) (Writer, error) {
	asc := AscWriterEnabled() && refs[0].StorageBackend == StorageBackendAsc
	for _, ref := range refs[1:] {
		if (AscWriterEnabled() && ref.StorageBackend == StorageBackendAsc) != asc {
			return nil, fmt.Errorf("%w: a request cannot span an asc repository and another storage backend", ErrUnsupported)
		}
	}
	if !asc {
		return r.git, nil
	}
	return Get(AscName)
}

func (r *router) CreateCommit(ctx context.Context, req CommitRequest) (*CommitResult, error) {
	refs := []RepoRef{req.Repo}
	if req.Update != nil {
		refs = append(refs, req.Update.Repo)
	}
	w, err := r.pick(refs...)
	if err != nil {
		return nil, err
	}
	return w.CreateCommit(ctx, req)
}

func (r *router) Merge(ctx context.Context, req MergeRequest) (*MergeResult, error) {
	refs := []RepoRef{req.Base, req.Head}
	if req.Publish != nil {
		refs = append(refs, req.Publish.Repo)
	}
	w, err := r.pick(refs...)
	if err != nil {
		return nil, err
	}
	return w.Merge(ctx, req)
}

func (r *router) CherryPick(ctx context.Context, req PickRequest) (*CommitResult, error) {
	refs := []RepoRef{req.Repo}
	if req.Update != nil {
		refs = append(refs, req.Update.Repo)
	}
	w, err := r.pick(refs...)
	if err != nil {
		return nil, err
	}
	return w.CherryPick(ctx, req)
}

func (r *router) Revert(ctx context.Context, req PickRequest) (*CommitResult, error) {
	refs := []RepoRef{req.Repo}
	if req.Update != nil {
		refs = append(refs, req.Update.Repo)
	}
	w, err := r.pick(refs...)
	if err != nil {
		return nil, err
	}
	return w.Revert(ctx, req)
}

func (r *router) ApplyPatch(ctx context.Context, req PatchRequest) (*CommitResult, error) {
	refs := []RepoRef{req.Repo}
	if req.Update != nil {
		refs = append(refs, req.Update.Repo)
	}
	w, err := r.pick(refs...)
	if err != nil {
		return nil, err
	}
	return w.ApplyPatch(ctx, req)
}

func (r *router) UpdateRefs(ctx context.Context, req UpdateRefsRequest) ([]RefResult, error) {
	refs := []RepoRef{req.Repo}
	if req.From != nil {
		refs = append(refs, *req.From)
	}
	w, err := r.pick(refs...)
	if err != nil {
		return nil, err
	}
	return w.UpdateRefs(ctx, req)
}

func (r *router) Init(ctx context.Context, req InitRequest) error {
	w, err := r.pick(req.Repo)
	if err != nil {
		return err
	}
	return w.Init(ctx, req)
}

func (r *router) Fork(ctx context.Context, req ForkRequest) error {
	w, err := r.pick(req.Repo, req.From)
	if err != nil {
		return err
	}
	return w.Fork(ctx, req)
}

func (r *router) Delete(ctx context.Context, repo RepoRef) error {
	w, err := r.pick(repo)
	if err != nil {
		return err
	}
	return w.Delete(ctx, repo)
}

func (r *router) Size(ctx context.Context, repo RepoRef) (Size, error) {
	w, err := r.pick(repo)
	if err != nil {
		return Size{}, err
	}
	return w.Size(ctx, repo)
}
