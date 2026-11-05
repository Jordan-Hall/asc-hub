// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package ascratelimit

import (
	"container/list"
	"sync"
	"time"
)

// Lockout counts the failures of a key, for the wait after wrong passwords: once limit tries were counted within
// wait of the first, the key is refused for wait. Every call takes the limits, so they follow the settings.
//
// Its table has a size limit. When it is full it drops the key whose count started the longest ago among those not
// waiting; a key that waits is never dropped, so a flood of names cannot end a wait. When every key waits, a new key
// counts against one shared overflow count with OverflowFactor times the limit: the keys already counted go on as
// before, and the tries of new keys stay bounded together instead of being refused one by one or not counted.
// OverflowFactor is how many times the limit of a key all new keys may try together while the table is full of waits.
const OverflowFactor = 100

type Lockout struct {
	mu      sync.Mutex
	items   map[string]*list.Element // key -> element of order, whose value is *lockItem
	order   *list.List               // oldest count start at the front
	over    lockItem                 // the shared count of new keys while the table is full of waits
	maxKeys int
	now     func() time.Time
}

type lockItem struct {
	key   string
	start time.Time
	n     int
	limit int
	until time.Time
}

// NewLockout makes an empty lockout table.
func NewLockout() *Lockout {
	return &Lockout{items: map[string]*list.Element{}, order: list.New(), maxKeys: DefaultMaxKeys, now: time.Now}
}

// Left is how long key still waits; 0 when it may try.
func (l *Lockout) Left(key string) time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()
	if el, ok := l.items[key]; ok {
		return max(el.Value.(*lockItem).until.Sub(l.now()), 0)
	}
	if len(l.items) >= l.maxKeys {
		return max(l.over.until.Sub(l.now()), 0)
	}
	return 0
}

// Begin is Left and, when key may try, counts the try before it is made, so that tries sent at the same time cannot
// pass the limit. The try that reaches limit starts the wait; Clear or Release it when the try succeeds. limit 0
// counts nothing.
func (l *Lockout) Begin(key string, limit int, wait time.Duration) time.Duration {
	if limit <= 0 {
		return 0
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	el, ok := l.items[key]
	if !ok {
		if len(l.items) >= l.maxKeys && !l.dropOne(now) {
			return countTry(&l.over, limit*OverflowFactor, wait, now)
		}
		el = l.order.PushBack(&lockItem{key: key, start: now})
		l.items[key] = el
	}
	it := el.Value.(*lockItem)
	if fresh(it, wait, now) {
		l.order.MoveToBack(el)
	}
	return countTry(it, limit, wait, now)
}

// countTry counts one try of it before it is made and gives how long it still waits (0: the try may be made).
func countTry(it *lockItem, limit int, wait time.Duration, now time.Time) time.Duration {
	if left := it.until.Sub(now); left > 0 {
		return left
	}
	if fresh(it, wait, now) {
		it.start, it.n, it.until = now, 0, time.Time{}
	}
	it.n++
	it.limit = limit
	if it.n < limit {
		return 0
	}
	// the last try may still be made; the wait applies from the next one
	it.until = now.Add(wait)
	return 0
}

// fresh says whether the next try of it starts a fresh count: its wait or its window is over.
func fresh(it *lockItem, wait time.Duration, now time.Time) bool {
	if !it.until.IsZero() {
		return !it.until.After(now)
	}
	return now.Sub(it.start) >= wait
}

// dropOne drops the key whose count started the longest ago among those not waiting; false when every key waits.
func (l *Lockout) dropOne(now time.Time) bool {
	for el := l.order.Front(); el != nil; el = el.Next() {
		if it := el.Value.(*lockItem); !it.until.After(now) {
			delete(l.items, it.key)
			l.order.Remove(el)
			return true
		}
	}
	return false
}

// Release gives back one try that Begin counted and that turned out not to count (it succeeded, or the server
// failed). When that try started the wait, the wait ends again.
func (l *Lockout) Release(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	it := &l.over // a key not in the table was counted in the overflow
	if el, ok := l.items[key]; ok {
		it = el.Value.(*lockItem)
	}
	it.n = max(it.n-1, 0)
	if it.n < it.limit {
		it.until = time.Time{}
	}
}

// Clear forgets key: its try succeeded.
func (l *Lockout) Clear(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if el, ok := l.items[key]; ok {
		delete(l.items, key)
		l.order.Remove(el)
	}
}
