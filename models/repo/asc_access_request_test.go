// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo_test

import (
	"strings"
	"testing"

	"forgejo.org/models/perm"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unittest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAscAccessRequests(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	ctx := t.Context()

	t.Run("The mode asked for follows the access the person has", func(t *testing.T) {
		assert.Equal(t, perm.AccessModeWrite, repo_model.AscAccessRequestMode(perm.AccessModeNone))
		assert.Equal(t, perm.AccessModeWrite, repo_model.AscAccessRequestMode(perm.AccessModeRead))
		assert.Equal(t, perm.AccessModeAdmin, repo_model.AscAccessRequestMode(perm.AccessModeWrite))
		assert.Equal(t, perm.AccessModeNone, repo_model.AscAccessRequestMode(perm.AccessModeAdmin))
		assert.Equal(t, perm.AccessModeNone, repo_model.AscAccessRequestMode(perm.AccessModeOwner))
	})

	t.Run("A second request replaces the first", func(t *testing.T) {
		require.NoError(t, repo_model.AscRequestAccess(ctx, 1, 5, perm.AccessModeWrite, "first"))
		require.NoError(t, repo_model.AscRequestAccess(ctx, 1, 5, perm.AccessModeAdmin, "second"))
		list, err := repo_model.ListAscAccessRequests(ctx, 1)
		require.NoError(t, err)
		require.Len(t, list, 1)
		assert.Equal(t, perm.AccessModeAdmin, list[0].Mode)
		assert.Equal(t, "second", list[0].Message)
		assert.Positive(t, int64(list[0].CreatedUnix))
	})

	t.Run("A long message is cut to the limit, by characters", func(t *testing.T) {
		long := strings.Repeat("é", repo_model.AscAccessRequestMessageMax+20)
		require.NoError(t, repo_model.AscRequestAccess(ctx, 1, 4, perm.AccessModeWrite, long))
		r, err := repo_model.GetAscAccessRequest(ctx, 1, 4)
		require.NoError(t, err)
		require.NotNil(t, r)
		assert.Len(t, []rune(r.Message), repo_model.AscAccessRequestMessageMax)
	})

	t.Run("A request is found by its id only within its repository", func(t *testing.T) {
		r, err := repo_model.GetAscAccessRequest(ctx, 1, 5)
		require.NoError(t, err)
		got, err := repo_model.GetAscAccessRequestByID(ctx, 1, r.ID)
		require.NoError(t, err)
		require.NotNil(t, got)
		assert.Equal(t, int64(5), got.UserID)
		other, err := repo_model.GetAscAccessRequestByID(ctx, 2, r.ID)
		require.NoError(t, err)
		assert.Nil(t, other)
	})

	t.Run("The requests of a repository are listed oldest first and removed one by one", func(t *testing.T) {
		list, err := repo_model.ListAscAccessRequests(ctx, 1)
		require.NoError(t, err)
		require.Len(t, list, 2)
		assert.Equal(t, []int64{5, 4}, []int64{list[0].UserID, list[1].UserID})
		require.NoError(t, repo_model.DeleteAscAccessRequest(ctx, 1, 5))
		require.NoError(t, repo_model.DeleteAscAccessRequest(ctx, 1, 5), "removing a missing request is not an error")
		r, err := repo_model.GetAscAccessRequest(ctx, 1, 5)
		require.NoError(t, err)
		assert.Nil(t, r)
		empty, err := repo_model.ListAscAccessRequests(ctx, 3)
		require.NoError(t, err)
		assert.Empty(t, empty)
	})
}
