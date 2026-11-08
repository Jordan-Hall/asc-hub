// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package git

import (
	"testing"
	"time"

	"forgejo.org/models/db"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unittest"
	"forgejo.org/modules/timeutil"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAscRemoveAndRestoreRule(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	ctx := db.DefaultContext
	repo := unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{ID: 1})
	start := time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)
	timeutil.MockSet(start)
	defer timeutil.MockUnset()

	rule := &ProtectedBranch{
		RepoID: repo.ID, RuleName: "release/*", RequiredApprovals: 2,
		EnableStatusCheck: true, StatusCheckContexts: []string{"ci", "lint"},
		WhitelistUserIDs: []int64{2}, ProtectedFilePatterns: "*.lock",
	}
	require.NoError(t, db.Insert(ctx, rule))
	require.NoError(t, AscRemoveRule(ctx, repo, rule, 2))

	gone, err := GetProtectedBranchRuleByName(ctx, repo.ID, "release/*")
	require.NoError(t, err)
	assert.Nil(t, gone)
	removed, err := AscRemovedRules(ctx, repo.ID)
	require.NoError(t, err)
	require.Len(t, removed, 1)
	assert.Equal(t, "release/*", removed[0].RuleName)
	assert.EqualValues(t, 2, removed[0].RemovedBy)
	assert.Equal(t, timeutil.TimeStamp(start.Add(AscRuleUndoWindow).Unix()), removed[0].Until())
	other, err := AscRemovedRules(ctx, 2)
	require.NoError(t, err)
	assert.Empty(t, other)

	// Another repository's removal is not this one's to undo.
	repo2 := unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{ID: 2})
	_, err = AscRestoreRule(ctx, repo2, removed[0].ID)
	require.ErrorIs(t, err, ErrAscRuleUndoGone)

	timeutil.MockSet(start.Add(29 * time.Minute))
	back, err := AscRestoreRule(ctx, repo, removed[0].ID)
	require.NoError(t, err)
	got, err := GetProtectedBranchRuleByName(ctx, repo.ID, "release/*")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, back.ID, got.ID)
	assert.EqualValues(t, 2, got.RequiredApprovals)
	assert.True(t, got.EnableStatusCheck)
	assert.Equal(t, []string{"ci", "lint"}, got.StatusCheckContexts)
	assert.Equal(t, []int64{2}, got.WhitelistUserIDs)
	assert.Equal(t, "*.lock", got.ProtectedFilePatterns)
	removed, err = AscRemovedRules(ctx, repo.ID)
	require.NoError(t, err)
	assert.Empty(t, removed, "a removal is undone once")
}

func TestAscRestoreRuleRefusals(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	ctx := db.DefaultContext
	repo := unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{ID: 1})
	start := time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)
	timeutil.MockSet(start)
	defer timeutil.MockUnset()

	remove := func(name string) int64 {
		rule := &ProtectedBranch{RepoID: repo.ID, RuleName: name}
		require.NoError(t, db.Insert(ctx, rule))
		require.NoError(t, AscRemoveRule(ctx, repo, rule, 2))
		rows, err := AscRemovedRules(ctx, repo.ID)
		require.NoError(t, err)
		return rows[0].ID
	}

	// Past the window.
	late := remove("late")
	timeutil.MockSet(start.Add(AscRuleUndoWindow + time.Second))
	_, err := AscRestoreRule(ctx, repo, late)
	require.ErrorIs(t, err, ErrAscRuleUndoGone)
	rows, err := AscRemovedRules(ctx, repo.ID)
	require.NoError(t, err)
	assert.Empty(t, rows, "a removal past the window is not listed")

	// The next removal drops the expired copy.
	taken := remove("taken")
	assert.False(t, unittest.BeanExists(t, &AscRemovedRule{ID: late}))

	// The same name was added since.
	require.NoError(t, db.Insert(ctx, &ProtectedBranch{RepoID: repo.ID, RuleName: "taken"}))
	_, err = AscRestoreRule(ctx, repo, taken)
	require.ErrorAs(t, err, &ErrAscRuleUndoTaken{})
	assert.True(t, unittest.BeanExists(t, &AscRemovedRule{ID: taken}), "a refused undo keeps the removal")

	// A rule added since overlaps.
	wide := remove("feature/x")
	require.NoError(t, db.Insert(ctx, &ProtectedBranch{RepoID: repo.ID, RuleName: "feature/*"}))
	_, err = AscRestoreRule(ctx, repo, wide)
	var overlap ErrAscRuleUndoOverlap
	require.ErrorAs(t, err, &overlap)
	assert.Equal(t, ErrAscRuleUndoOverlap{Name: "feature/x", With: "feature/*"}, overlap)

	_, err = AscRestoreRule(ctx, repo, 0)
	require.ErrorIs(t, err, ErrAscRuleUndoGone)
}
