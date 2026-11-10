// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package stats

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"forgejo.org/modules/git"

	"github.com/stretchr/testify/assert"
)

type fakeIndexer struct {
	errs    map[int64]error
	indexed []int64
}

func (f *fakeIndexer) Index(id int64) error {
	f.indexed = append(f.indexed, id)
	return f.errs[id]
}

func (f *fakeIndexer) Close() {}

// A repository whose backend is not up yet goes back to the queue; one that is indexed or fails for another reason
// does not.
func TestHandlerHandsBackTheRepositoriesThatWaitForTheBackend(t *testing.T) {
	saved := indexer
	t.Cleanup(func() { indexer = saved })
	fake := &fakeIndexer{errs: map[int64]error{
		2: fmt.Errorf("stat: %w", git.ErrBackendUnavailable),
		3: errors.New("broken"),
		4: git.ErrBackendUnavailable,
	}}
	indexer = fake

	assert.Equal(t, []int64{2, 4}, handler(1, 2, 3, 4))
	assert.Equal(t, []int64{1, 2, 3, 4}, fake.indexed)
	assert.Empty(t, handler(1, 3))
}

// The log says that repositories wait at most once a minute, however often the queue tries again.
func TestNoteWaitingIsRateLimited(t *testing.T) {
	lastWaitNotice.Store(0)
	t.Cleanup(func() { lastWaitNotice.Store(0) })
	start := time.Unix(1_800_000_000, 0)
	assert.True(t, noteWaiting(3, start))
	assert.False(t, noteWaiting(3, start.Add(5*time.Second)))
	assert.False(t, noteWaiting(3, start.Add(waitNoticeEvery-time.Second)))
	assert.True(t, noteWaiting(1, start.Add(waitNoticeEvery)))
}
