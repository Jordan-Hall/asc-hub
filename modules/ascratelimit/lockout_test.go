// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package ascratelimit

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// begin is Begin without the ticket.
func begin(l *Lockout, key string, limit int, wait time.Duration) time.Duration {
	left, _ := l.Begin(key, limit, wait)
	return left
}

func TestLockoutBeginCountsTheTryBeforeItIsMade(t *testing.T) {
	l := NewLockout()
	at := time.Unix(1000, 0)
	l.now = func() time.Time { return at }
	for i := 0; i < 3; i++ {
		assert.Zero(t, begin(l, "a", 3, time.Minute), "try %d may be made", i)
	}
	assert.Equal(t, time.Minute, l.Left("a"), "the third try started the wait")
	assert.Equal(t, time.Minute, begin(l, "a", 3, time.Minute), "the fourth waits")
	assert.Zero(t, l.Left("b"), "another key has its own count")

	at = at.Add(40 * time.Second)
	assert.Equal(t, 20*time.Second, l.Left("a"))
	at = at.Add(20 * time.Second)
	assert.Zero(t, begin(l, "a", 3, time.Minute), "the wait is over and the count starts again")
	assert.Zero(t, l.Left("a"))
}

func TestLockoutClearAndWindow(t *testing.T) {
	l := NewLockout()
	at := time.Unix(1000, 0)
	l.now = func() time.Time { return at }
	begin(l, "a", 3, time.Minute)
	begin(l, "a", 3, time.Minute)
	l.Clear("a")
	assert.Zero(t, begin(l, "a", 3, time.Minute))
	assert.Zero(t, begin(l, "a", 3, time.Minute))
	assert.Zero(t, l.Left("a"), "two tries after the clear")

	at = at.Add(time.Minute)
	assert.Zero(t, begin(l, "a", 3, time.Minute), "a try after the window starts a fresh count")
	assert.Zero(t, begin(l, "a", 3, time.Minute))
	assert.Zero(t, l.Left("a"))
}

func TestLockoutReleaseGivesTheTryBack(t *testing.T) {
	l := NewLockout()
	at := time.Unix(1000, 0)
	l.now = func() time.Time { return at }
	_, try := l.Begin("ip", 2, time.Minute)
	l.Release(try)
	assert.Zero(t, begin(l, "ip", 2, time.Minute))
	assert.Zero(t, l.Left("ip"), "the given back try did not count")
	left, try := l.Begin("ip", 2, time.Minute)
	assert.Zero(t, left)
	assert.Equal(t, time.Minute, l.Left("ip"), "the try that reaches the limit starts the wait")
	l.Release(try)
	assert.Zero(t, l.Left("ip"), "giving that try back ends the wait")
	l.Release(Ticket{})

	// a ticket only gives back the try it counted
	_, old := l.Begin("ip", 2, time.Minute)
	at = at.Add(2 * time.Minute)
	assert.Zero(t, begin(l, "ip", 2, time.Minute), "a fresh count")
	assert.Zero(t, begin(l, "ip", 2, time.Minute))
	l.Release(old)
	assert.Equal(t, time.Minute, l.Left("ip"), "a ticket of the old count gives nothing back")
	_, gone := l.Begin("x", 5, time.Minute)
	l.Clear("x")
	assert.Zero(t, begin(l, "x", 1, time.Minute))
	l.Release(gone)
	assert.Equal(t, time.Minute, l.Left("x"), "a ticket of a dropped key gives nothing back")
}

func TestLockoutOffCountsNothing(t *testing.T) {
	l := NewLockout()
	for i := 0; i < 10; i++ {
		assert.Zero(t, begin(l, "a", 0, time.Minute))
	}
	assert.Zero(t, l.Left("a"))
}

func TestLockoutTriesAtTheSameTimeCannotPassTheLimit(t *testing.T) {
	l := NewLockout()
	var wg sync.WaitGroup
	var mu sync.Mutex
	allowed := 0
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if begin(l, "a", 5, time.Minute) == 0 {
				mu.Lock()
				allowed++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	assert.Equal(t, 5, allowed)
}

func TestLockoutTableIsBounded(t *testing.T) {
	l := NewLockout()
	l.maxKeys = 4
	for i := 0; i < 10; i++ {
		begin(l, fmt.Sprint(i), 3, time.Minute)
	}
	assert.Len(t, l.items, 4)
	assert.Equal(t, 4, l.order.Len())
}

func TestLockoutTableKeepsTheWaits(t *testing.T) {
	l := NewLockout()
	l.maxKeys = 3
	at := time.Unix(1000, 0)
	l.now = func() time.Time { return at }
	begin(l, "victim", 1, time.Minute)
	for i := 0; i < 10; i++ {
		begin(l, fmt.Sprint(i), 3, time.Minute)
	}
	assert.Equal(t, time.Minute, l.Left("victim"), "a flood of keys that do not wait does not end a wait")
	assert.Len(t, l.items, 3)

	at = at.Add(10 * time.Second)
	begin(l, "other", 1, time.Minute)
	at = at.Add(10 * time.Second)
	begin(l, "third", 1, time.Minute)
	assert.Len(t, l.items, 3)

	// when every key waits, the one whose wait ends soonest is dropped and a new key has its own count
	at = at.Add(10 * time.Second)
	assert.Zero(t, begin(l, "new", 3, time.Minute), "a new key may try")
	assert.Zero(t, l.Left("victim"), "the wait nearest its end was dropped")
	assert.Equal(t, 40*time.Second, l.Left("other"), "the other waits are kept")
	assert.Equal(t, 50*time.Second, l.Left("third"))
	assert.Zero(t, begin(l, "another", 3, time.Minute), "and a further new key is not refused")
	assert.Len(t, l.items, 3)
}
