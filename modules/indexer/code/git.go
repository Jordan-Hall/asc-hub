// Copyright 2019 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package code

import (
	"context"
	"fmt"
	"path"
	"strings"

	repo_model "forgejo.org/models/repo"
	"forgejo.org/modules/git"
	"forgejo.org/modules/gitrepo"
	"forgejo.org/modules/indexer/code/internal"
	"forgejo.org/modules/log"
	"forgejo.org/modules/setting"
)

func getDefaultBranchSha(ctx context.Context, repo *repo_model.Repository) (string, error) {
	gitRepo, err := gitrepo.OpenRepository(ctx, repo)
	if err != nil {
		return "", err
	}
	defer gitRepo.Close()

	return gitRepo.GetBranchCommitID(repo.DefaultBranch)
}

// getRepoChanges returns changes to repo since last indexer update
func getRepoChanges(ctx context.Context, repo *repo_model.Repository, revision string) (*internal.RepoChanges, error) {
	status, err := repo_model.GetIndexerStatus(ctx, repo, repo_model.RepoIndexerTypeCode)
	if err != nil {
		return nil, err
	}

	gitRepo, err := gitrepo.OpenRepository(ctx, repo)
	if err != nil {
		return nil, err
	}
	defer gitRepo.Close()

	needGenesis := len(status.CommitSha) == 0
	if !needGenesis {
		// An unknown last indexed commit (for example after a force push) or no common history both mean a full rebuild.
		mergeBase, err := gitRepo.GetMergeBaseSimple(status.CommitSha, revision)
		needGenesis = err != nil || len(mergeBase) == 0
	}

	if needGenesis {
		return genesisChanges(gitRepo, revision)
	}
	return nonGenesisChanges(ctx, gitRepo, repo, revision)
}

// isIndexable reports whether the file at filename (its path from the repository root) is worth indexing.
func isIndexable(filename string, entry *git.TreeEntry) bool {
	if !entry.IsRegular() && !entry.IsExecutable() {
		return false
	}
	name := strings.ToLower(filename)
	for _, g := range setting.Indexer.ExcludePatterns {
		if g.Match(name) {
			return false
		}
	}
	for _, g := range setting.Indexer.IncludePatterns {
		if g.Match(name) {
			return true
		}
	}
	return len(setting.Indexer.IncludePatterns) == 0
}

// indexableUpdates turns tree entries into file updates, keeping only the indexable files.
func indexableUpdates(entries []*git.TreeEntry) []internal.FileUpdate {
	updates := make([]internal.FileUpdate, 0, len(entries))
	for _, entry := range entries {
		if isIndexable(entry.Name(), entry) {
			updates = append(updates, fileUpdate(entry.Name(), entry, entry.Size()))
		}
	}
	return updates
}

// fileUpdate describes entry, which sits at filename from the repository root.
func fileUpdate(filename string, entry *git.TreeEntry, size int64) internal.FileUpdate {
	return internal.FileUpdate{
		Filename: filename,
		BlobSha:  entry.ID.String(),
		Size:     size,
		Sized:    true,
	}
}

// blobSizes reads the size of every entry in one batch. A missing object is an error: the index must not guess a size.
func blobSizes(ctx context.Context, gitRepo *git.Repository, entries []*git.TreeEntry) ([]int64, error) {
	if len(entries) == 0 {
		return nil, nil
	}
	be, err := gitRepo.Backend()
	if err != nil {
		return nil, err
	}
	ids := make([]git.ObjectID, len(entries))
	for i, entry := range entries {
		ids[i] = entry.ID
	}
	infos, err := be.StatObjects(ctx, ids)
	if err != nil {
		return nil, err
	}
	if len(infos) != len(ids) {
		return nil, fmt.Errorf("stat of %d objects returned %d results", len(ids), len(infos))
	}
	sizes := make([]int64, len(infos))
	for i, info := range infos {
		if info.Missing {
			return nil, fmt.Errorf("object %s of %q is missing", info.ID, entries[i].Name())
		}
		sizes[i] = info.Size
	}
	return sizes, nil
}

// genesisChanges get changes to add repo to the indexer for the first time
func genesisChanges(gitRepo *git.Repository, revision string) (*internal.RepoChanges, error) {
	commit, err := gitRepo.GetCommit(revision)
	if err != nil {
		return nil, err
	}
	entries, err := commit.Tree.ListEntriesRecursiveWithSize()
	if err != nil {
		return nil, err
	}
	return &internal.RepoChanges{Updates: indexableUpdates(entries)}, nil
}

// nonGenesisChanges get changes since the previous indexer update
func nonGenesisChanges(ctx context.Context, gitRepo *git.Repository, repo *repo_model.Repository, revision string) (*internal.RepoChanges, error) {
	changed, err := gitRepo.GetFilesChangedBetween(repo.CodeIndexerStatus.CommitSha, revision)
	if err != nil {
		// previous commit sha may have been removed by a force push, so
		// try rebuilding from scratch
		log.Warn("git diff: %v", err)
		if err := (*globalIndexer.Load()).Delete(ctx, repo.ID); err != nil {
			return nil, err
		}
		return genesisChanges(gitRepo, revision)
	}

	commit, err := gitRepo.GetCommit(revision)
	if err != nil {
		return nil, err
	}
	head := &headTree{root: &commit.Tree, dirs: map[string]map[string]*git.TreeEntry{}}

	var (
		changes   internal.RepoChanges
		indexable []*git.TreeEntry
		paths     []string
	)
	for _, filename := range changed {
		entry, err := head.lookup(filename)
		if err != nil {
			return nil, err
		}
		switch {
		case entry == nil, !entry.IsRegular() && !entry.IsExecutable():
			// Deleted, renamed away, or replaced by a directory, symlink or submodule: nothing to index any more.
			changes.RemovedFilenames = append(changes.RemovedFilenames, filename)
		case isIndexable(filename, entry):
			indexable = append(indexable, entry)
			paths = append(paths, filename)
		}
	}

	sizes, err := blobSizes(ctx, gitRepo, indexable)
	if err != nil {
		return nil, err
	}
	for i, entry := range indexable {
		changes.Updates = append(changes.Updates, fileUpdate(paths[i], entry, sizes[i]))
	}
	return &changes, nil
}

// headTree finds entries by path in one tree, listing each directory once.
type headTree struct {
	root *git.Tree
	// dirs maps a directory path to its entries by name; a nil value records a directory that does not exist.
	dirs map[string]map[string]*git.TreeEntry
}

// lookup returns the entry at filename, or nil when there is none.
func (h *headTree) lookup(filename string) (*git.TreeEntry, error) {
	dir, name := path.Split(filename)
	entries, err := h.entries(path.Clean(dir))
	if err != nil {
		return nil, err
	}
	return entries[name], nil
}

func (h *headTree) entries(dir string) (map[string]*git.TreeEntry, error) {
	if dir == "." || dir == "/" {
		dir = ""
	}
	if entries, ok := h.dirs[dir]; ok {
		return entries, nil
	}
	tree := h.root
	if dir != "" {
		parentDir, name := path.Split(dir)
		parent, err := h.entries(path.Clean(parentDir))
		if err != nil {
			return nil, err
		}
		entry := parent[name]
		if entry == nil || !entry.IsDir() {
			h.dirs[dir] = nil
			return nil, nil
		}
		if tree = entry.Tree(); tree == nil {
			return nil, fmt.Errorf("unable to read tree %q", dir)
		}
	}
	list, err := tree.ListEntries()
	if err != nil {
		return nil, err
	}
	entries := make(map[string]*git.TreeEntry, len(list))
	for _, entry := range list {
		entries[entry.Name()] = entry
	}
	h.dirs[dir] = entries
	return entries, nil
}
