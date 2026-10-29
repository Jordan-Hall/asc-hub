// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package repository

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unittest"
	"forgejo.org/modules/repostore"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The size of a repository stored in asc can take minutes right after a large import. The page must not wait for
// it, and the answer must still reach the database.
func TestUpdateRepoSizeOfAnAscRepositoryFinishesInTheBackground(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	release := make(chan struct{})
	var calls atomic.Int32
	oldSize := ascSize
	ascSize = func(context.Context, *repo_model.Repository) (repostore.Size, error) {
		calls.Add(1)
		<-release
		return repostore.Size{Logical: 4242}, nil
	}
	t.Cleanup(func() { ascSize = oldSize })
	oldWait := ascSizeWait
	ascSizeWait = 50 * time.Millisecond
	t.Cleanup(func() { ascSizeWait = oldWait })

	repo := unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{ID: 1})
	repo.StorageBackend = "asc"

	start := time.Now()
	require.NoError(t, UpdateRepoSize(t.Context(), repo))
	assert.Less(t, time.Since(start), 2*time.Second, "the caller did not wait for asc")

	// A second request while the first still runs starts nothing.
	require.NoError(t, UpdateRepoSize(t.Context(), repo))

	close(release)
	require.Eventually(t, func() bool {
		r := unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{ID: 1})
		return r.Size == 4242
	}, 5*time.Second, 20*time.Millisecond)
	assert.Equal(t, int32(1), calls.Load())
}
