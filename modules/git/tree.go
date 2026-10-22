// Copyright 2015 The Gogs Authors. All rights reserved.
// Copyright 2019 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package git

import (
	"bytes"
	"errors"
	"strings"
)

// Tree represents a flat directory listing.
type Tree struct {
	ID         ObjectID
	ResolvedID ObjectID
	repo       *Repository

	// parent tree
	ptree *Tree

	entries       Entries
	entriesParsed bool

	entriesRecursive       Entries
	entriesRecursiveParsed bool
}

// NewTree create a new tree according the repository and tree id
func NewTree(repo *Repository, id ObjectID) *Tree {
	return &Tree{
		ID:   id,
		repo: repo,
	}
}

// ListEntries returns all entries of current tree.
func (t *Tree) ListEntries() (Entries, error) {
	if t.entriesParsed {
		return t.entries, nil
	}
	if t.repo == nil {
		return nil, errors.New("tree has no repository")
	}

	be, err := t.repo.Backend()
	if err != nil {
		return nil, err
	}
	data, err := be.ReadTree(t.repo.Ctx, t.ID)
	if err != nil {
		return nil, err
	}
	t.entries = t.entriesFromData(data)
	t.entriesParsed = true
	return t.entries, nil
}

// entriesFromData builds the entries of this tree from what a Backend read.
func (t *Tree) entriesFromData(data []TreeEntryData) Entries {
	entries := make(Entries, len(data))
	for i, d := range data {
		entry := &TreeEntry{ID: d.ID, ptree: t, entryMode: d.Mode, name: d.Name}
		if d.Size >= 0 {
			entry.size = d.Size
			entry.sized = true
		}
		entries[i] = entry
	}
	return entries
}

// listEntriesRecursive returns all entries of current tree recursively including all subtrees
// withSizes loads the size of each blob, which is slower
func (t *Tree) listEntriesRecursive(withSizes bool) (Entries, error) {
	if t.entriesRecursiveParsed {
		return t.entriesRecursive, nil
	}
	if t.repo == nil {
		return nil, errors.New("tree has no repository")
	}

	be, err := t.repo.Backend()
	if err != nil {
		return nil, err
	}
	var data []TreeEntryData
	for entry, err := range be.ReadTreeRecursive(t.repo.Ctx, t.ID, TreeWalkOpts{Trees: true, Sizes: withSizes}) {
		if err != nil {
			return nil, err
		}
		data = append(data, entry)
	}
	t.entriesRecursive = t.entriesFromData(data)
	t.entriesRecursiveParsed = true
	return t.entriesRecursive, nil
}

// ListEntriesRecursiveFast returns all entries of current tree recursively including all subtrees, no size
func (t *Tree) ListEntriesRecursiveFast() (Entries, error) {
	return t.listEntriesRecursive(false)
}

// ListEntriesRecursiveWithSize returns all entries of current tree recursively including all subtrees, with size
func (t *Tree) ListEntriesRecursiveWithSize() (Entries, error) {
	return t.listEntriesRecursive(true)
}

// SubTree get a sub tree by the sub dir path
func (t *Tree) SubTree(rpath string) (*Tree, error) {
	if len(rpath) == 0 {
		return t, nil
	}

	paths := strings.Split(rpath, "/")
	var (
		err error
		g   = t
		p   = t
		te  *TreeEntry
	)
	for _, name := range paths {
		te, err = p.GetTreeEntryByPath(name)
		if err != nil {
			return nil, err
		}

		g, err = t.repo.getTree(te.ID)
		if err != nil {
			return nil, err
		}
		g.ptree = p
		p = g
	}
	return g, nil
}

// LsTree checks if the given filenames are in the tree
func (repo *Repository) LsTree(ref string, filenames ...string) ([]string, error) {
	cmd := NewCommand(repo.Ctx, "ls-tree", "-z", "--name-only").
		AddDashesAndList(append([]string{ref}, filenames...)...)

	res, _, err := cmd.RunStdBytes(&RunOpts{Dir: repo.Path})
	if err != nil {
		return nil, err
	}
	filelist := make([]string, 0, len(filenames))
	for line := range bytes.SplitSeq(res, []byte{'\000'}) {
		filelist = append(filelist, string(line))
	}

	return filelist, err
}

// GetTreePathLatestCommitID returns the latest commit of a tree path
func (repo *Repository) GetTreePathLatestCommit(refName, treePath string) (*Commit, error) {
	stdout, _, err := NewCommand(repo.Ctx, "rev-list", "-1").
		AddDynamicArguments(refName).AddDashesAndList(treePath).
		RunStdString(&RunOpts{Dir: repo.Path})
	if err != nil {
		return nil, err
	}
	return repo.GetCommit(strings.TrimSpace(stdout))
}
