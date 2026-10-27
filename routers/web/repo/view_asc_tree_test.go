// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func fakeAscTree(listed *[]string) ascTreeLister {
	dirs := map[string][]ascTreeEntry{
		"": {{"README.md", false}, {"src", true}, {"Cargo.toml", false}, {"docs", true}, {"Tests", true}},
		"src": {{"retry.rs", false}, {"client.rs", false}, {"metrics", true}},
		"src/metrics": {{"mod.rs", false}},
		"docs":        {{"retries.md", false}},
		"Tests":       {{"a.rs", false}},
	}
	return func(dir string) ([]ascTreeEntry, error) {
		*listed = append(*listed, dir)
		e, ok := dirs[dir]
		if !ok {
			return nil, errors.New("not a directory")
		}
		return append([]ascTreeEntry(nil), e...), nil
	}
}

func ascTreeLink(p string) string { return "/r/src/branch/main/" + p }

func names(nodes []*AscTreeNode) []string {
	out := make([]string, 0, len(nodes))
	for _, n := range nodes {
		out = append(out, n.Name)
	}
	return out
}

func TestBuildAscTreeOpensOnlyThePath(t *testing.T) {
	var listed []string
	nodes, _, err := buildAscTree(fakeAscTree(&listed), "src/retry.rs", false, ascTreeLink)
	require.NoError(t, err)

	// directories first, then files, each case-insensitively by name
	assert.Equal(t, []string{"docs", "src", "Tests", "Cargo.toml", "README.md"}, names(nodes))
	// only the root and the directories on the way are listed
	assert.Equal(t, []string{"", "src"}, listed)

	var src *AscTreeNode
	for _, n := range nodes {
		if n.Name == "src" {
			src = n
		}
		if n.Name != "src" {
			assert.False(t, n.Open, n.Name)
			assert.Empty(t, n.Children, n.Name)
		}
	}
	require.NotNil(t, src)
	assert.True(t, src.Open)
	assert.False(t, src.Current)
	assert.Equal(t, []string{"metrics", "client.rs", "retry.rs"}, names(src.Children))
	last := src.Children[2]
	assert.True(t, last.Current)
	assert.Equal(t, "/r/src/branch/main/src/retry.rs", last.Link)
	assert.False(t, src.Children[0].Open)
}

func TestBuildAscTreeOpensTheShownDirectory(t *testing.T) {
	var listed []string
	nodes, _, err := buildAscTree(fakeAscTree(&listed), "src/metrics", true, ascTreeLink)
	require.NoError(t, err)
	assert.Equal(t, []string{"", "src", "src/metrics"}, listed)
	var metrics *AscTreeNode
	for _, n := range nodes {
		if n.Name == "src" {
			metrics = n.Children[0]
		}
	}
	require.NotNil(t, metrics)
	assert.True(t, metrics.Current)
	assert.True(t, metrics.Open)
	assert.Equal(t, []string{"mod.rs"}, names(metrics.Children))
}

func TestBuildAscTreeUnknownPathAndErrors(t *testing.T) {
	var listed []string
	nodes, _, err := buildAscTree(fakeAscTree(&listed), "nope/x", false, ascTreeLink)
	require.NoError(t, err)
	assert.Equal(t, []string{""}, listed, "a path that is not in the tree opens nothing")
	for _, n := range nodes {
		assert.False(t, n.Open)
		assert.False(t, n.Current)
	}

	_, _, err = buildAscTree(func(string) ([]ascTreeEntry, error) { return nil, errors.New("boom") }, "a", false, ascTreeLink)
	require.Error(t, err)
}

func TestBuildAscTreeCutsLongDirectories(t *testing.T) {
	long := make([]ascTreeEntry, 0, ascTreeMaxEntries+30)
	for i := range ascTreeMaxEntries + 30 {
		long = append(long, ascTreeEntry{Name: fmt.Sprintf("f%04d", i)})
	}
	long = append(long, ascTreeEntry{Name: "dir", IsDir: true})
	list := func(dir string) ([]ascTreeEntry, error) {
		if dir == "" {
			return long, nil
		}
		return nil, errors.New("unexpected")
	}
	nodes, cut, err := buildAscTree(list, "", true, ascTreeLink)
	require.NoError(t, err)
	assert.True(t, cut)
	assert.Len(t, nodes, ascTreeMaxEntries)
	assert.Equal(t, "dir", nodes[0].Name, "directories sort before files, so the cut drops files")
}
