// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package asc

import (
	"context"
	"errors"
	"fmt"
	"math"

	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/repostore"

	"google.golang.org/grpc/codes"
)

// admin sends one RepoAdmin call.
func (w *Writer) admin(ctx context.Context, op string, ref repostore.RepoRef, build func(*ascv1.RepoAdminRequest)) (*ascv1.RepoAdminResponse, error) {
	h, err := handle(ref)
	if err != nil {
		return nil, err
	}
	s, err := w.begin(ctx)
	if err != nil {
		return nil, err
	}
	defer s.close()
	req := &ascv1.RepoAdminRequest{Repo: h.ascRef()}
	build(req)
	resp, err := s.api.RepoAdmin(s.ctx, req)
	if err != nil {
		return nil, callErr(op, err)
	}
	return resp, nil
}

// Init creates an empty repository (a new namespace). It fails with ErrExists when the repository exists.
func (w *Writer) Init(ctx context.Context, req repostore.InitRequest) error {
	if err := req.Validate(); err != nil {
		return err
	}
	_, err := w.admin(ctx, "RepoAdmin.init", req.Repo, func(r *ascv1.RepoAdminRequest) {
		r.Op = &ascv1.RepoAdminRequest_Init{Init: &ascv1.InitOp{DefaultBranch: req.DefaultBranch, Mode: "primary"}}
	})
	if err != nil {
		return err
	}
	if b := w.bridgeHandler(); b != nil {
		return b.Prepare(ctx, req.Repo, req.DefaultBranch)
	}
	return nil
}

// Fork creates a repository that holds a copy of another: a namespace copy, not a clone. It fails with
// ErrExists when the target exists.
func (w *Writer) Fork(ctx context.Context, req repostore.ForkRequest) error {
	if err := req.Validate(); err != nil {
		return err
	}
	if req.SingleBranch != "" {
		return fmt.Errorf("%w: a fork of one branch", repostore.ErrUnsupported)
	}
	from, err := handle(req.From)
	if err != nil {
		return err
	}
	_, err = w.admin(ctx, "RepoAdmin.fork", req.Repo, func(r *ascv1.RepoAdminRequest) {
		r.Op = &ascv1.RepoAdminRequest_Fork{Fork: &ascv1.ForkOp{From: from.ascRef()}}
	})
	if err != nil {
		return err
	}
	if b := w.bridgeHandler(); b != nil {
		return b.Prepare(ctx, req.Repo, "")
	}
	return nil
}

// Delete removes a repository: asc drops its refs now and its namespace after 30 days. Deleting a repository
// that does not exist is not an error.
func (w *Writer) Delete(ctx context.Context, repo repostore.RepoRef) error {
	_, err := w.admin(ctx, "RepoAdmin.delete", repo, func(r *ascv1.RepoAdminRequest) {
		r.Op = &ascv1.RepoAdminRequest_Delete{Delete: true}
	})
	var ce *Error
	if err != nil && !(errors.As(err, &ce) && ce.Code == codes.NotFound) {
		return err
	}
	// Cleaning up after a repository that is already gone still removes its directory.
	if b := w.bridgeHandler(); b != nil {
		return b.Remove(ctx, repo)
	}
	return nil
}

// Size reports the logical size (bytes reachable from refs, what counts against a quota) and the unique size
// (what the namespace alone occupies).
func (w *Writer) Size(ctx context.Context, repo repostore.RepoRef) (repostore.Size, error) {
	resp, err := w.admin(ctx, "RepoAdmin.size", repo, func(r *ascv1.RepoAdminRequest) {
		r.Op = &ascv1.RepoAdminRequest_Size{Size: true}
	})
	if err != nil {
		return repostore.Size{}, err
	}
	return repostore.Size{Logical: clampInt64(resp.GetLogicalSize()), Unique: clampInt64(resp.GetUniqueSize())}, nil
}

// Verify runs the repository doctor (RepoAdmin.verify): every ref's closure exists and the git map round-trips.
// It returns asc's report.
func (w *Writer) Verify(ctx context.Context, repo repostore.RepoRef) (string, error) {
	resp, err := w.admin(ctx, "RepoAdmin.verify", repo, func(r *ascv1.RepoAdminRequest) {
		r.Op = &ascv1.RepoAdminRequest_Verify{Verify: true}
	})
	if err != nil {
		return "", err
	}
	return resp.GetReport(), nil
}

// Verify runs the repository doctor of the registered Writer (see Writer.Verify).
func Verify(ctx context.Context, repo repostore.RepoRef) (string, error) {
	return shared.Verify(ctx, repo)
}

// Rename renames a repository in asc's registry (owner and name; RepoAdmin.rename). to is the new repository.
func (w *Writer) Rename(ctx context.Context, repo, to repostore.RepoRef) error {
	target, err := handle(to)
	if err != nil {
		return err
	}
	_, err = w.admin(ctx, "RepoAdmin.rename", repo, func(r *ascv1.RepoAdminRequest) {
		r.Op = &ascv1.RepoAdminRequest_RenameTo{RenameTo: target.name}
	})
	if err != nil {
		return err
	}
	if b := w.bridgeHandler(); b != nil {
		return b.Rename(ctx, repo, to)
	}
	return nil
}

func clampInt64(v uint64) int64 {
	if v > math.MaxInt64 {
		return math.MaxInt64
	}
	return int64(v)
}
