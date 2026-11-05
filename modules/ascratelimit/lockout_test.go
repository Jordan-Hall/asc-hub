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

func TestLockoutFailCountsAfterTheTry(t *testing.T) {
	l := NewLockout()
	at := time.Unix(1000, 0)
	l.now = func() time.Time { return at }
	assert.Zero(t, l.Fail("ip", 2, time.Minute))
	assert.Equal(t, time.Minute, l.Fail("ip", 2, time.Minute), "the failure that reaches the limit starts the wait")
	assert.Equal(t, time.Minute, l.Left("ip"))
}

func TestLockoutOffCountsNothing(t *testing.T) {
	l := NewLockout()
	for i := 0; i < 10; i++ {
		assert.Zero(t, l.Begin("a", 0, time.Minute))
		assert.Zero(t, l.Fail("a", 0, time.Minute))
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
		l.Fail(fmt.Sprint(i), 3, time.Minute)
	}
	assert.Len(t, l.items, 4)
	assert.Equal(t, 4, l.order.Len())
}
