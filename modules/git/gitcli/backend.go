// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: MIT

// Package gitcli is the git.Backend that runs the git binary on a bare repository.
//
// It is the backend for legacy repositories and tests. The code in this package
// moved, mostly verbatim, out of the git.Repository methods that now dispatch to
// a git.Backend. Link the package in (a blank import is enough) to register it as
// git.DefaultBackendName.
package gitcli

import (
	"context"

	"forgejo.org/modules/git"
	"forgejo.org/modules/log"
)

// Name is the name this backend registers under.
const Name = git.DefaultBackendName

// Limits that bound the work and memory a single untrusted object or listing can cost.
const (
	// maxMetaObjectSize caps a commit or tag object read into memory.
	maxMetaObjectSize = 64 << 20
	// maxTreeListing caps the output of one ls-tree run.
	maxTreeListing = 1 << 30
	// statBatchSize is the number of ids written to cat-file --batch-check before its answers are read.
	statBatchSize = 256
	// maxTagDepth bounds the chain of tags peeled to find a commit.
	maxTagDepth = 32
)

func init() {
	git.RegisterBackend(Name, New)
}

// Backend serves one repository through the git binary. It shares the cat-file
// batch processes of the git.Repository it is bound to, so it has the same
// concurrency rule as that Repository: one goroutine at a time.
type Backend struct {
	repo *git.Repository
}

var _ git.Backend = (*Backend)(nil)

// New binds a Backend to repo. It starts no process.
func New(repo *git.Repository) (git.Backend, error) {
	return &Backend{repo: repo}, nil
}

// ObjectFormat returns the object format of the repository, or sha1 when it cannot be read.
func (b *Backend) ObjectFormat() git.ObjectFormat {
	format, err := b.repo.GetObjectFormat()
	if err != nil {
		log.Debug("gitcli: object format of %s: %v", b.repo.Path, err)
		return git.Sha1ObjectFormat
	}
	return format
}

// Close is a no-op: the batch processes belong to the git.Repository and are closed by it.
func (b *Backend) Close() error {
	return nil
}

// batchCtx is the context a cat-file batch process is started with. The process is shared and outlives
// one call, so it takes the context of the Repository, not the context of the call that happened to start it.
func (b *Backend) batchCtx(ctx context.Context) context.Context {
	if b.repo.Ctx != nil {
		return b.repo.Ctx
	}
	return ctx
}
