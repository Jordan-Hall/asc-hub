// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package issues_test

import (
	"testing"

	"forgejo.org/models/db"
	issues_model "forgejo.org/models/issues"
	"forgejo.org/models/unittest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAscOpenPullsByBase(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	// repo1 has three open pull requests (fixtures): two into master, one into branch2
	by, err := issues_model.AscOpenPullsByBase(db.DefaultContext, 1)
	require.NoError(t, err)
	assert.Equal(t, map[string]int64{"master": 2, "branch2": 1}, by)

	_, err = db.GetEngine(db.DefaultContext).ID(2).Cols("is_closed").Update(&issues_model.Issue{IsClosed: true})
	require.NoError(t, err)
	by, err = issues_model.AscOpenPullsByBase(db.DefaultContext, 1)
	require.NoError(t, err)
	assert.Equal(t, map[string]int64{"master": 1, "branch2": 1}, by, "a closed pull request does not count")

	by, err = issues_model.AscOpenPullsByBase(db.DefaultContext, 999)
	require.NoError(t, err)
	assert.Empty(t, by)
}
