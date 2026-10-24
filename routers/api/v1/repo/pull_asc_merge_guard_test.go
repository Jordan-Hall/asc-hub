// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	"net/http"
	"strings"
	"testing"

	"forgejo.org/models/db"
	issues_model "forgejo.org/models/issues"
	"forgejo.org/models/unittest"
	"forgejo.org/services/contexttest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAscMergeGuard(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	change := strings.Repeat("k", 32)
	child := unittest.AssertExistsAndLoadBean(t, &issues_model.PullRequest{ID: 5})
	parent := unittest.AssertExistsAndLoadBean(t, &issues_model.PullRequest{ID: 2})
	require.False(t, parent.HasMerged)

	// A pull request that is not stacked is not blocked.
	ctx, resp := contexttest.MockAPIContext(t, "user2/repo1/pulls/5/merge")
	assert.False(t, ascMergeGuard(ctx, child))
	assert.Equal(t, http.StatusOK, resp.Code)

	// A stacked pull request whose parent has not merged answers 409 stack-parent-unmerged.
	require.NoError(t, issues_model.SetPullRequestAscStack(t.Context(), child, change, parent.ID))
	ctx, resp = contexttest.MockAPIContext(t, "user2/repo1/pulls/5/merge")
	assert.True(t, ascMergeGuard(ctx, child))
	assert.Equal(t, http.StatusConflict, resp.Code)
	assert.Contains(t, resp.Body.String(), `"message":"stack-parent-unmerged"`)

	// The bottom of the stack is not blocked.
	require.NoError(t, issues_model.SetPullRequestAscStack(t.Context(), child, change, 0))
	ctx, resp = contexttest.MockAPIContext(t, "user2/repo1/pulls/5/merge")
	assert.False(t, ascMergeGuard(ctx, child))
	assert.Equal(t, http.StatusOK, resp.Code)

	// Once the parent has merged the child may merge.
	require.NoError(t, issues_model.SetPullRequestAscStack(t.Context(), child, change, parent.ID))
	_, err := db.GetEngine(t.Context()).Exec("UPDATE `pull_request` SET has_merged = ? WHERE id = ?", true, parent.ID)
	require.NoError(t, err)
	ctx, resp = contexttest.MockAPIContext(t, "user2/repo1/pulls/5/merge")
	assert.False(t, ascMergeGuard(ctx, child))
	assert.Equal(t, http.StatusOK, resp.Code)
}
