// Copyright 2019 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package repository

import (
	"context"
	"errors"
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
	index := newTemplateIndex(entries)
	gt, err := checkGiteaTemplate(index)
	if err != nil {
		return fmt.Errorf("checkGiteaTemplate: %w", err)
	}
	var plan map[string]expansionPlan
	if gt != nil {
		plan, err = planExpansion(index, gt)
		if err != nil {
			return fmt.Errorf("planExpansion: %w", err)
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
		if plan[path].expand {
			content = &expandedBlob{
				blob:   blob,
				expand: func(src string) string { return generateExpansion(src, templateRepo, generateRepo, false) },
				lazy:   lazy,
			}
		}
		if plan[path].rename {
			// Substitute filename variables
			path = generateExpansion(path, templateRepo, generateRepo, true)
		}
		// The git directory of a repository is not part of a commit, whatever a variable made of a path.
		if isGitDirPath(path) {
			continue
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

// isGitDirPath reports whether a path is, or is in, the git directory at the root of a work tree.
func isGitDirPath(p string) bool {
	first, _, _ := strings.Cut(p, "/")
	return strings.EqualFold(first, ".git")
}

// expansionPlan says what the template does to the file at a path of the template.
type expansionPlan struct {
	// rename: the variables in the path are expanded (the path matches a glob of the template).
	rename bool
	// expand: the variables in the content are expanded.
	expand bool
}

// planExpansion decides, for every file the globs of the template match, which paths are renamed and which
// contents are expanded. A matched symbolic link keeps its own content: the variables are expanded in the file it
// points to, as when the template is written through the link on disk. A link that leaves the template is an error.
func planExpansion(index *templateIndex, gt *GiteaTemplate) (map[string]expansionPlan, error) {
	globs := gt.Globs()
	plan := map[string]expansionPlan{}
	if len(globs) == 0 {
		return plan, nil
	}
	for p, entry := range index.files {
		if p == gt.Path || entry.Mode() == git.EntryModeCommit {
			continue
		}
		matched := false
		for _, g := range globs {
			if g.Match(p) {
				matched = true
				break
			}
		}
		if !matched {
			continue
		}
		target := p
		if entry.Mode() == git.EntryModeSymlink {
			_, resolved, err := index.resolve("openat", p)
			if err != nil {
				return nil, err
			}
			target = resolved
		}
		own := plan[p]
		own.rename = true
		plan[p] = own
		other := plan[target]
		other.expand = true
		plan[target] = other
	}
	return plan, nil
}

// Limits on the symbolic links of a template.
const (
	// maxTemplateSymlinkHops is the most links followed to reach a file, the limit of the Linux kernel.
	maxTemplateSymlinkHops = 40
	// maxTemplateSymlinkBytes is the longest target of a link.
	maxTemplateSymlinkBytes = 4096
)

var (
	errTemplatePathEscapes = errors.New("path escapes from parent")
	errTemplateNotExist    = errors.New("file does not exist")
	errTemplateNotFile     = errors.New("not a regular file")
	errTemplateTooManyLink = errors.New("too many levels of symbolic links")
)

// templateIndex finds the files of a template by path, and follows its symbolic links without leaving it.
type templateIndex struct {
	files map[string]*git.TreeEntry
	dirs  map[string]struct{}
}

func newTemplateIndex(entries git.Entries) *templateIndex {
	index := &templateIndex{files: make(map[string]*git.TreeEntry, len(entries)), dirs: map[string]struct{}{}}
	for _, entry := range entries {
		if entry.IsDir() {
			index.dirs[entry.Name()] = struct{}{}
			continue
		}
		index.files[entry.Name()] = entry
	}
	return index
}

// resolve follows the symbolic links on the way to a path, and returns the regular file at its end and the path of
// that file in the template. An absolute link, or a path with more ".." than directories, leaves the template and is
// an error. op is only used in the text of an error.
func (ix *templateIndex) resolve(op, start string) (*git.TreeEntry, string, error) {
	fail := func(err error) (*git.TreeEntry, string, error) {
		return nil, "", fmt.Errorf("%s %s: %w", op, start, err)
	}
	pending := strings.Split(start, "/")
	var current []string
	hops := 0
	for len(pending) > 0 {
		name := pending[0]
		pending = pending[1:]
		switch name {
		case "", ".":
			continue
		case "..":
			if len(current) == 0 {
				return fail(errTemplatePathEscapes)
			}
			current = current[:len(current)-1]
			continue
		}
		next := strings.Join(append(current[:len(current):len(current)], name), "/")
		if entry, ok := ix.files[next]; ok {
			if entry.Mode() != git.EntryModeSymlink {
				if len(pending) > 0 {
					return fail(errTemplateNotExist)
				}
				current = append(current, name)
				continue
			}
			hops++
			if hops > maxTemplateSymlinkHops {
				return fail(errTemplateTooManyLink)
			}
			target, err := readSmallBlob(entry, maxTemplateSymlinkBytes)
			if err != nil {
				return fail(err)
			}
			if strings.HasPrefix(target, "/") {
				return fail(errTemplatePathEscapes)
			}
			pending = append(strings.Split(target, "/"), pending...)
			continue
		}
		if _, ok := ix.dirs[next]; ok {
			current = append(current, name)
			continue
		}
		return fail(errTemplateNotExist)
	}
	resolved := strings.Join(current, "/")
	entry, ok := ix.files[resolved]
	if !ok || (entry.Mode() != git.EntryModeBlob && entry.Mode() != git.EntryModeExec) {
		return fail(errTemplateNotFile)
	}
	return entry, resolved, nil
}

// readSmallBlob reads a blob that is at most limit bytes.
func readSmallBlob(entry *git.TreeEntry, limit int64) (string, error) {
	blob := entry.Blob()
	if blob.Size() > limit {
		return "", fmt.Errorf("larger than %d bytes", limit)
	}
	rc, err := blob.DataAsync()
	if err != nil {
		return "", err
	}
	content, err := io.ReadAll(io.LimitReader(rc, limit+1))
	_ = rc.Close()
	if err != nil {
		return "", err
	}
	if int64(len(content)) > limit {
		return "", fmt.Errorf("larger than %d bytes", limit)
	}
	return string(content), nil
}

// checkGiteaTemplate returns the template configuration of a template repository, or nil when it has none. The
// configuration may be a link to a file in the template.
func checkGiteaTemplate(index *templateIndex) (*GiteaTemplate, error) {
	for _, dir := range []string{".forgejo", ".gitea"} {
		candidate := dir + "/template"
		entry, ok := index.files[candidate]
		if !ok {
			continue
		}
		if entry.Mode() == git.EntryModeSymlink {
			resolved, _, err := index.resolve("statat", candidate)
			if errors.Is(err, errTemplateNotExist) {
				continue
			}
			if err != nil {
				return nil, err
			}
			entry = resolved
		}
		// A configuration that is not a plain file is not read.
		if entry.Mode() != git.EntryModeBlob && entry.Mode() != git.EntryModeExec {
			continue
		}
		if entry.Blob().Size() > maxTemplateConfigBytes {
			return nil, fmt.Errorf("%s is larger than %d bytes", candidate, maxTemplateConfigBytes)
		}
		content, err := readSmallBlob(entry, maxTemplateConfigBytes)
		if err != nil {
			return nil, err
		}
		return &GiteaTemplate{Path: candidate, Content: []byte(content)}, nil
	}
	return nil, nil
}
