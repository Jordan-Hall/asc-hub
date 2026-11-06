// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	"errors"
	"testing"

	issues_model "forgejo.org/models/issues"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAscTouchedPaths(t *testing.T) {
	prs := []*issues_model.PullRequest{{ID: 1}, {ID: 2}, {ID: 3}}
	changed := map[int64][]string{
		1: {"src/retry.rs", "src/client.rs", "src/retry.rs"},
		2: {"src/retry.rs", ""},
	}
	touched := ascTouchedPaths(prs, func(pr *issues_model.PullRequest) ([]string, error) {
		if pr.ID == 3 {
			return nil, errors.New("head is gone")
		}
		return changed[pr.ID], nil
	})
	assert.Equal(t, map[string]int{"src/retry.rs": 2, "src/client.rs": 1}, touched, "a path counts once per stack; an unreadable stack is left out")
}

func TestAscTouchedPathsReadsAtMostTheCap(t *testing.T) {
	prs := make([]*issues_model.PullRequest, ascTreeMaxStacks+10)
	for i := range prs {
		prs[i] = &issues_model.PullRequest{ID: int64(i + 1)}
	}
	read := 0
	touched := ascTouchedPaths(prs, func(*issues_model.PullRequest) ([]string, error) {
		read++
		return []string{"a"}, nil
	})
	assert.Equal(t, ascTreeMaxStacks, read)
	assert.Equal(t, ascTreeMaxStacks, touched["a"])
}

func TestMarkAscTreeTouched(t *testing.T) {
	var listed []string
	nodes, _, err := buildAscTree(fakeAscTree(&listed), "src/retry.rs", false, ascTreeLink)
	require.NoError(t, err)
	n := markAscTreeTouched(nodes, map[string]int{"src/retry.rs": 2, "src/client.rs": 1, "src": 3, "docs/retries.md": 1})
	assert.Equal(t, 2, n, "only shown files count: docs is closed and a directory has no count")

	var src *AscTreeNode
	for _, node := range nodes {
		if node.Name == "src" {
			src = node
		}
		assert.Zero(t, node.Stacks, node.Path)
	}
	require.NotNil(t, src)
	got := map[string]int{}
	for _, c := range src.Children {
		got[c.Name] = c.Stacks
	}
	assert.Equal(t, map[string]int{"retry.rs": 2, "client.rs": 1, "metrics": 0}, got)
}
