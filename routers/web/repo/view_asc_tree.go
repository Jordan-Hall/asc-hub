// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	"sort"
	"strings"

	"forgejo.org/modules/log"
	"forgejo.org/modules/util"
	"forgejo.org/services/context"
)

// ascTreeMaxEntries bounds the entries listed for one directory of the folder rail. A directory with more entries
// is cut and says so: the rail is a map for moving around, the file list shows everything.
const ascTreeMaxEntries = 200

// AscTreeNode is one line of the folder rail on the code pages (design board Code): a directory on the way to the
// shown path with its entries, or an entry beside them.
type AscTreeNode struct {
	Name     string
	Path     string
	Link     string
	IsDir    bool
	Open     bool // a directory on the way to the shown path
	Current  bool // the shown path itself
	Children []*AscTreeNode
	// Cut is true on a directory whose list was cut at ascTreeMaxEntries.
	Cut bool
}

// ascTreeEntry is what the builder needs to know about a tree entry.
type ascTreeEntry struct {
	Name  string
	IsDir bool
}

// ascTreeLister lists the entries of the directory at dir ("" is the root).
type ascTreeLister func(dir string) ([]ascTreeEntry, error)

// buildAscTree builds the nodes of the root directory with the directories on the way to treePath opened. Only
// those directories are listed, so the cost follows the depth of the path, not the size of the repository.
// treePath is the path that is shown ("" for none); isDir says whether it is a directory.
func buildAscTree(list ascTreeLister, treePath string, isDir bool, link func(path string) string) ([]*AscTreeNode, bool, error) {
	var names []string
	if treePath != "" {
		names = strings.Split(treePath, "/")
	}
	var build func(dir string, depth int) ([]*AscTreeNode, bool, error)
	build = func(dir string, depth int) ([]*AscTreeNode, bool, error) {
		entries, err := list(dir)
		if err != nil {
			return nil, false, err
		}
		sort.SliceStable(entries, func(i, j int) bool {
			if entries[i].IsDir != entries[j].IsDir {
				return entries[i].IsDir
			}
			return strings.ToLower(entries[i].Name) < strings.ToLower(entries[j].Name)
		})
		cut := false
		if len(entries) > ascTreeMaxEntries {
			entries, cut = entries[:ascTreeMaxEntries], true
		}
		nodes := make([]*AscTreeNode, 0, len(entries))
		for _, e := range entries {
			p := e.Name
			if dir != "" {
				p = dir + "/" + e.Name
			}
			n := &AscTreeNode{Name: e.Name, Path: p, Link: link(p), IsDir: e.IsDir}
			onPath := depth < len(names) && names[depth] == e.Name
			if onPath {
				if depth == len(names)-1 {
					n.Current = true
					n.Open = e.IsDir && isDir
				} else {
					n.Open = e.IsDir
				}
			}
			if n.IsDir && n.Open {
				children, childCut, err := build(p, depth+1)
				if err != nil {
					return nil, false, err
				}
				n.Children, n.Cut = children, childCut
			}
			nodes = append(nodes, n)
		}
		return nodes, cut, nil
	}
	return build("", 0)
}

// prepareAscTree puts the folder rail of the shown path into ctx.Data. A failure to read the tree leaves the rail
// out; the page does not depend on it.
func prepareAscTree(ctx *context.Context, isDir bool) {
	if ctx.Repo.Commit == nil || ctx.Repo.TreePath == "" {
		return
	}
	commit := ctx.Repo.Commit
	list := func(dir string) ([]ascTreeEntry, error) {
		tree := &commit.Tree
		if dir != "" {
			sub, err := commit.SubTree(dir)
			if err != nil {
				return nil, err
			}
			tree = sub
		}
		entries, err := tree.ListEntries()
		if err != nil {
			return nil, err
		}
		out := make([]ascTreeEntry, 0, len(entries))
		for _, e := range entries {
			if e.IsSubmodule() {
				continue
			}
			out = append(out, ascTreeEntry{Name: e.Name(), IsDir: e.IsDir()})
		}
		return out, nil
	}
	branchLink := ctx.Repo.RepoLink + "/src/" + ctx.Repo.BranchNameSubURL()
	nodes, cut, err := buildAscTree(list, ctx.Repo.TreePath, isDir, func(p string) string {
		return branchLink + "/" + util.PathEscapeSegments(p)
	})
	if err != nil {
		log.Warn("the folder rail of %s is left out: %v", ctx.Repo.Repository.FullName(), err)
		return
	}
	ctx.Data["AscTree"] = nodes
	ctx.Data["AscTreeCut"] = cut
}
