// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package automerge

import (
	"context"
	"testing"

	"forgejo.org/models/db"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unittest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The mirror hears about the entries that are in the state now, and only those.
func TestAscQueueMirrorHearsTheEntriesInTheStateOnly(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	var got []string
	t.Cleanup(UseAscQueueMirror(func(_ context.Context, repo *repo_model.Repository, e *AscStackQueueEntry, state string) {
		got = append(got, state+":"+e.AscChange)
	}))
	a := &AscStackQueueEntry{RepoID: 1, Branch: "master", PullID: 9001, AscChange: "kkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkk", DoerID: 2, State: AscQueueLanding}
	b := &AscStackQueueEntry{RepoID: 1, Branch: "master", PullID: 9002, AscChange: "llllllllllllllllllllllllllllllll", DoerID: 2, State: AscQueueQueued}
	_, err := db.GetEngine(t.Context()).Insert(a, b)
	require.NoError(t, err)

	ascQueueMirrorState(t.Context(), AscQueueLanding, []int64{9001, 9002})
	assert.Equal(t, []string{"landing:kkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkk"}, got, "an entry the update skipped is not announced")

	got = nil
	ascQueueMirrorState(t.Context(), AscQueueMerged, nil)
	ascQueueMirrorState(t.Context(), AscQueueQueued, []int64{9002})
	assert.Equal(t, []string{"queued:llllllllllllllllllllllllllllllll"}, got)
}

func TestAscQueueChangeRaw(t *testing.T) {
	assert.Nil(t, ascChangeRaw("short"))
	assert.Nil(t, ascChangeRaw("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"))
	assert.Len(t, ascChangeRaw("kkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkk"), 16)
}
