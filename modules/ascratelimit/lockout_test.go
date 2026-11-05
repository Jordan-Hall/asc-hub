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

func TestLockoutBeginCountsTheTryBeforeItIsMade(t *testing.T) {
	l := NewLockout()
	at := time.Unix(1000, 0)
	l.now = func() time.Time { return at }
	for i := 0; i < 3; i++ {
		assert.Zero(t, l.Begin("a", 3, time.Minute), "try %d may be made", i)
	}
	assert.Equal(t, time.Minute, l.Left("a"), "the third try started the wait")
	assert.Equal(t, time.Minute, l.Begin("a", 3, time.Minute), "the fourth waits")
	assert.Zero(t, l.Left("b"), "another key has its own count")

	at = at.Add(40 * time.Second)
	assert.Equal(t, 20*time.Second, l.Left("a"))
	at = at.Add(20 * time.Second)
	assert.Zero(t, l.Begin("a", 3, time.Minute), "the wait is over and the count starts again")
	assert.Zero(t, l.Left("a"))
}

func TestLockoutClearAndWindow(t *testing.T) {
	l := NewLockout()
	at := time.Unix(1000, 0)
	l.now = func() time.Time { return at }
	l.Begin("a", 3, time.Minute)
	l.Begin("a", 3, time.Minute)
	l.Clear("a")
	assert.Zero(t, l.Begin("a", 3, time.Minute))
	assert.Zero(t, l.Begin("a", 3, time.Minute))
	assert.Zero(t, l.Left("a"), "two tries after the clear")

	at = at.Add(time.Minute)
	assert.Zero(t, l.Begin("a", 3, time.Minute), "a try after the window starts a fresh count")
	assert.Zero(t, l.Begin("a", 3, time.Minute))
	assert.Zero(t, l.Left("a"))
}

func TestLockoutReleaseGivesTheTryBack(t *testing.T) {
	l := NewLockout()
	at := time.Unix(1000, 0)
	l.now = func() time.Time { return at }
	assert.Zero(t, l.Begin("ip", 2, time.Minute))
	l.Release("ip")
	assert.Zero(t, l.Begin("ip", 2, time.Minute))
	assert.Zero(t, l.Left("ip"), "the given back try did not count")
	assert.Zero(t, l.Begin("ip", 2, time.Minute))
	assert.Equal(t, time.Minute, l.Left("ip"), "the try that reaches the limit starts the wait")
	l.Release("ip")
	assert.Zero(t, l.Left("ip"), "giving that try back ends the wait")
	l.Release("unknown")
}

func TestLockoutOffCountsNothing(t *testing.T) {
	l := NewLockout()
	for i := 0; i < 10; i++ {
		assert.Zero(t, l.Begin("a", 0, time.Minute))
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
			if l.Begin("a", 5, time.Minute) == 0 {
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
		l.Begin(fmt.Sprint(i), 3, time.Minute)
	}
	assert.Len(t, l.items, 4)
	assert.Equal(t, 4, l.order.Len())
}

func TestLockoutTableNeverDropsAWait(t *testing.T) {
	l := NewLockout()
	l.maxKeys = 3
	at := time.Unix(1000, 0)
	l.now = func() time.Time { return at }
	l.Begin("victim", 1, time.Minute)
	for i := 0; i < 10; i++ {
		l.Begin(fmt.Sprint(i), 3, time.Minute)
	}
	assert.Equal(t, time.Minute, l.Left("victim"), "a flood of new keys does not end a wait")
	assert.Len(t, l.items, 3)

	l.Begin("other", 1, time.Minute)
	l.Begin("third", 1, time.Minute)
	assert.Equal(t, time.Minute, l.Begin("new", 3, time.Minute), "when every key waits a new key is refused")
	assert.Zero(t, l.Left("new"))

	at = at.Add(time.Minute)
	assert.Zero(t, l.Begin("new", 3, time.Minute), "a key whose wait is over may be dropped")
	assert.Len(t, l.items, 3)
}
