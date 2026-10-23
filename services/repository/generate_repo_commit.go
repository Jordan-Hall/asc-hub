// Copyright 2019 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package repository

import (
	"context"
	"fmt"
	"io"
	"strings"

	repo_model "forgejo.org/models/repo"
	"forgejo.org/modules/git"
	"forgejo.org/modules/gitrepo"
	"forgejo.org/modules/repostore"
)

// Limits on what a template can ask the server to hold in memory.
const (
	// maxTemplateConfigBytes is the largest .forgejo/template or .gitea/template file.
	maxTemplateConfigBytes = 1 << 20
	// maxTemplateExpandBytes is the largest file whose variables are expanded.
	maxTemplateExpandBytes = 64 << 20
)

// lazyBlob reads a blob of the template when it is first read, and closes it at its end. The writer reads the
// files of a commit one after the other, so at most one blob is open at a time, as the batch reader of the
// git module needs.
type lazyBlob struct {
	open func() (io.ReadCloser, error)
	rc   io.ReadCloser
	done bool
}

func (l *lazyBlob) Read(p []byte) (int, error) {
	if l.done {
		return 0, io.EOF
	}
	if l.rc == nil {
		rc, err := l.open()
		if err != nil {
			l.done = true
			return 0, err
		}
		l.rc = rc
	}
	n, err := l.rc.Read(p)
	if err != nil {
		_ = l.Close()
	}
	return n, err
}

// Close releases the blob. It is safe to call more than once.
func (l *lazyBlob) Close() error {
	l.done = true
	if l.rc == nil {
		return nil
	}
	rc := l.rc
	l.rc = nil
	return rc.Close()
}

// expandedBlob reads a blob of the template and expands the variables in it when it is first read.
type expandedBlob struct {
	blob   *git.Blob
	expand func(string) string
	lazy   *lazyBlob
	out    *strings.Reader
}

func (e *expandedBlob) Read(p []byte) (int, error) {
	if e.out == nil {
		if e.blob.Size() > maxTemplateExpandBytes {
			return 0, fmt.Errorf("template file is larger than %d bytes", maxTemplateExpandBytes)
		}
		data, err := io.ReadAll(io.LimitReader(e.lazy, maxTemplateExpandBytes+1))
		_ = e.lazy.Close()
		if err != nil {
			return 0, err
		}
		e.out = strings.NewReader(e.expand(string(data)))
	}
	return e.out.Read(p)
}

// generateRepoCommit makes the first commit of a repository generated from a template: the files of the default
// branch of the template, with the variables of the .forgejo/template (or .gitea/template) expanded.
func generateRepoCommit(ctx context.Context, repo, templateRepo, generateRepo *repo_model.Repository) error {
	gitRepo, err := gitrepo.OpenRepository(ctx, templateRepo)
	if err != nil {
		return fmt.Errorf("open template: %w", err)
	}
	defer gitRepo.Close()

	commit, err := gitRepo.GetBranchCommit(templateRepo.DefaultBranch)
	if err != nil {
		return fmt.Errorf("template branch: %w", err)
	}
	entries, err := commit.Tree.ListEntriesRecursiveFast()
	if err != nil {
		return fmt.Errorf("list template: %w", err)
	}

	// Variable expansion
	gt, err := checkGiteaTemplate(entries)
	if err != nil {
		return fmt.Errorf("checkGiteaTemplate: %w", err)
	}
	var globs func(string) bool
	if gt != nil {
		compiled := gt.Globs()
		globs = func(p string) bool {
			for _, g := range compiled {
				if g.Match(p) {
					return true
				}
			}
			return false
		}
	}

	var blobs []*lazyBlob
	defer func() {
		for _, b := range blobs {
			_ = b.Close()
		}
	}()

	ops := make([]repostore.FileOp, 0, len(entries))
	for _, entry := range entries {
		mode := entry.Mode()
		// Directories are implied by the files in them. A submodule has no content here, as in a clone.
		if entry.IsDir() || mode == git.EntryModeCommit {
			continue
		}
		path := entry.Name()
		if gt != nil && path == gt.Path {
			continue
		}

		blob := entry.Blob()
		lazy := &lazyBlob{open: blob.DataAsync}
		blobs = append(blobs, lazy)

		var content io.Reader = lazy
		if gt != nil && globs(path) {
			if mode != git.EntryModeSymlink {
				content = &expandedBlob{
					blob:   blob,
					expand: func(src string) string { return generateExpansion(src, templateRepo, generateRepo, false) },
					lazy:   lazy,
				}
			}
			// Substitute filename variables
			path = generateExpansion(path, templateRepo, generateRepo, true)
		}
		ops = append(ops, repostore.FileOp{Kind: repostore.OpPut, Path: path, Mode: mode, Content: content})
	}

	// set default branch based on whether it's specified in the newly generated repo or not
	defaultBranch := repo.DefaultBranch
	if strings.TrimSpace(defaultBranch) == "" {
		defaultBranch = templateRepo.DefaultBranch
	}

	return initRepoCommit(ctx, repo, repo.Owner, defaultBranch, ops)
}

// checkGiteaTemplate returns the template configuration of a template repository, or nil when it has none.
func checkGiteaTemplate(entries git.Entries) (*GiteaTemplate, error) {
	for _, dir := range []string{".forgejo", ".gitea"} {
		candidate := dir + "/template"
		for _, entry := range entries {
			if entry.Name() != candidate {
				continue
			}
			// A configuration that is not a plain file is not read.
			if entry.Mode() != git.EntryModeBlob && entry.Mode() != git.EntryModeExec {
				continue
			}
			blob := entry.Blob()
			if blob.Size() > maxTemplateConfigBytes {
				return nil, fmt.Errorf("%s is larger than %d bytes", candidate, maxTemplateConfigBytes)
			}
			rc, err := blob.DataAsync()
			if err != nil {
				return nil, err
			}
			content, err := io.ReadAll(io.LimitReader(rc, maxTemplateConfigBytes+1))
			_ = rc.Close()
			if err != nil {
				return nil, err
			}
			return &GiteaTemplate{Path: candidate, Content: content}, nil
		}
	}
	return nil, nil
}
