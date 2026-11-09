// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package issues_test

import (
	"testing"

	issues_model "forgejo.org/models/issues"
	"forgejo.org/models/unittest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPullRequestAscDirs(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())

	got, err := issues_model.GetPullRequestAscDirs(t.Context(), []int64{1, 2})
	require.NoError(t, err)
	assert.Empty(t, got)

	require.NoError(t, issues_model.SetPullRequestAscDirs(t.Context(), 1, "aaa", map[string]int{"src/pay/": 3}))
	require.NoError(t, issues_model.SetPullRequestAscDirs(t.Context(), 2, "bbb", nil))
	require.NoError(t, issues_model.SetPullRequestAscDirs(t.Context(), 1, "ccc", map[string]int{"tests/": 1}))

	got, err = issues_model.GetPullRequestAscDirs(t.Context(), []int64{1, 2, 3})
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Equal(t, "ccc", got[1].Head, "a new head replaces the old one")
	assert.Equal(t, map[string]int{"tests/": 1}, got[1].Dirs)
	assert.Empty(t, got[2].Dirs)
	unittest.AssertCount(t, &issues_model.PullRequestAscDirs{}, 2)
}
