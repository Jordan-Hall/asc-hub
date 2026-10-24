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
	pull_service "forgejo.org/services/pull"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAscStackPanel(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	change := strings.Repeat("k", 32)
	child := unittest.AssertExistsAndLoadBean(t, &issues_model.PullRequest{ID: 5})
	parent := unittest.AssertExistsAndLoadBean(t, &issues_model.PullRequest{ID: 2})

	// Not stacked: no panel, no block.
	ctx, resp := contexttest.MockContext(t, "user2/repo1/pulls/5")
	assert.True(t, prepareAscStackPanel(ctx, child))
	assert.Nil(t, ctx.Data["AscStackPanel"])
	assert.False(t, ascStackMergeBlocked(ctx, child))

	// Stacked on an open parent: the panel lists bottom to top, the merge is blocked.
	require.NoError(t, issues_model.SetPullRequestAscStack(t.Context(), parent, change[:31]+"l", 0))
	require.NoError(t, issues_model.SetPullRequestAscStack(t.Context(), child, change, parent.ID))
	ctx, resp = contexttest.MockContext(t, "user2/repo1/pulls/5")
	assert.True(t, prepareAscStackPanel(ctx, child))
	panel, ok := ctx.Data["AscStackPanel"].(*pull_service.AscStackPanel)
	require.True(t, ok)
	require.Len(t, panel.Entries, 2)
	assert.Equal(t, parent.Index, panel.Entries[0].Index)
	assert.False(t, panel.Entries[0].Current)
	assert.True(t, panel.Entries[1].Current)
	assert.True(t, panel.MergeBlocked)
	assert.False(t, panel.NeedsRestack)
	assert.True(t, ascStackMergeBlocked(ctx, child))
	assert.Equal(t, http.StatusBadRequest, resp.Code)

	// The parent has landed: no block, and the child needs a restack.
	_, err := db.GetEngine(t.Context()).Exec("UPDATE `pull_request` SET has_merged = ? WHERE id = ?", true, parent.ID)
	require.NoError(t, err)
	ctx, _ = contexttest.MockContext(t, "user2/repo1/pulls/5")
	assert.True(t, prepareAscStackPanel(ctx, child))
	panel = ctx.Data["AscStackPanel"].(*pull_service.AscStackPanel)
	assert.False(t, panel.MergeBlocked)
	assert.True(t, panel.NeedsRestack)
	assert.False(t, ascStackMergeBlocked(ctx, child))
}
