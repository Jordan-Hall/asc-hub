// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package mailer

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestTheQueueWatchKnowsTheOldestWaitingMail(t *testing.T) {
	w := newQueueWatch()
	t0 := time.Date(2026, 11, 9, 10, 0, 0, 0, time.UTC)
	a := &Message{Date: t0}
	b := &Message{Date: t0.Add(time.Minute)}
	twin := &Message{Date: t0}
	assert.True(t, w.oldest().IsZero())

	w.pushed(a)
	w.pushed(twin)
	w.pushed(b)
	w.pushed(&Message{}) // no write time: not watched
	assert.Equal(t, t0, w.oldest().UTC())

	now := t0.Add(2 * time.Minute)
	w.tried(a, nil, now)
	assert.Equal(t, t0, w.oldest().UTC(), "a mail written at the same moment still waits")
	w.tried(twin, errors.New("refused"), now.Add(time.Second))
	assert.Equal(t, t0.Add(time.Minute), w.oldest().UTC())
	assert.Equal(t, now, w.lastSent)
	assert.Equal(t, now.Add(time.Second), w.lastFailed)
	assert.True(t, QueueState{LastSent: w.lastSent, LastFailed: w.lastFailed}.Failing())

	// A mail an earlier run queued was never watched; trying it changes nothing else.
	w.tried(&Message{Date: t0.Add(-time.Hour)}, nil, now.Add(2*time.Second))
	assert.Equal(t, t0.Add(time.Minute), w.oldest().UTC())
	assert.False(t, QueueState{LastSent: now.Add(2 * time.Second), LastFailed: w.lastFailed}.Failing())
	w.tried(b, nil, now)
	assert.True(t, w.oldest().IsZero())
	assert.Empty(t, w.waiting)
}

func TestTheQueueWatchIsBounded(t *testing.T) {
	w := newQueueWatch()
	t0 := time.Date(2026, 11, 9, 10, 0, 0, 0, time.UTC)
	for i := range maxWatched + 5 {
		w.pushed(&Message{Date: t0.Add(time.Duration(i) * time.Millisecond)})
	}
	assert.Len(t, w.waiting, maxWatched)
	w.pushed(&Message{Date: t0})
	assert.Equal(t, 2, w.waiting[t0.UnixNano()], "a write time already watched is still counted")
	assert.Equal(t, t0, w.oldest().UTC())
}
