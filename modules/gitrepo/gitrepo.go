// Copyright 2024 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package gitrepo

import (
	"context"
	"io"
	"path/filepath"
	"strings"

	"forgejo.org/modules/git"
	_ "forgejo.org/modules/git/ascbackend"   // registers the asc backend that storage_backend=asc selects (configured by ascbackend.Init)
	_ "forgejo.org/modules/git/dual"         // registers the shadow-comparing backend that storage_backend=dual selects
	_ "forgejo.org/modules/git/gitcli"       // registers the default git backend that git.OpenRepository binds
	_ "forgejo.org/modules/repostore/gitcli" // registers the default writer that repostore.Default returns
	"forgejo.org/modules/setting"
)

type Repository interface {
	GetName() string
	GetOwnerName() string
}

func repoPath(repo Repository) string {
	return filepath.Join(setting.RepoRootPath, strings.ToLower(repo.GetOwnerName()), strings.ToLower(repo.GetName())+".git")
}

func wikiPath(repo Repository) string {
	return filepath.Join(setting.RepoRootPath, strings.ToLower(repo.GetOwnerName()), strings.ToLower(repo.GetName())+".wiki.git")
}

// NamedRepository identifies a repository by owner and name, for callers that
// do not have a loaded repository model (migration, F3).
type NamedRepository struct {
	Owner, Name string
}

func (r NamedRepository) GetName() string      { return r.Name }
func (r NamedRepository) GetOwnerName() string { return r.Owner }

// OpenRepository opens the repository with the git backend that its storage_backend selects (backend_select.go).
func OpenRepository(ctx context.Context, repo Repository) (*git.Repository, error) {
	name, err := selectBackend(repo)
	if err != nil {
		return nil, err
	}
	return git.OpenRepositoryWithBackend(ctx, repoPath(repo), name)
}

// OpenWikiRepository opens the wiki repository. A wiki is a separate bare git directory that the
// storage_backend column does not cover, so it always uses the default backend.
func OpenWikiRepository(ctx context.Context, repo Repository) (*git.Repository, error) {
	return git.OpenRepository(ctx, wikiPath(repo))
}

// contextKey is a value for use with context.WithValue.
type contextKey struct {
	name string
}

// RepositoryContextKey is a context key. It is used with context.Value() to get the current Repository for the context
var RepositoryContextKey = &contextKey{"repository"}

// RepositoryFromContext attempts to get the repository from the context
func repositoryFromContext(ctx context.Context, repo Repository) *git.Repository {
	value := ctx.Value(RepositoryContextKey)
	if value == nil {
		return nil
	}

	if gitRepo, ok := value.(*git.Repository); ok && gitRepo != nil {
		if gitRepo.Path == repoPath(repo) {
			return gitRepo
		}
	}

	return nil
}

type nopCloser func()

func (nopCloser) Close() error { return nil }

// RepositoryFromContextOrOpen attempts to get the repository from the context or just opens it
func RepositoryFromContextOrOpen(ctx context.Context, repo Repository) (*git.Repository, io.Closer, error) {
	gitRepo := repositoryFromContext(ctx, repo)
	if gitRepo != nil {
		return gitRepo, nopCloser(nil), nil
	}

	gitRepo, err := OpenRepository(ctx, repo)
	return gitRepo, gitRepo, err
}
