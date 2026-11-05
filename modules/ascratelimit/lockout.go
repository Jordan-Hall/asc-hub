// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package ascratelimit

import (
	"container/list"
	"sync"
	"time"
)

// Lockout counts the failures of a key, for the wait after wrong passwords: once max failures were counted within
// wait of the first, the key is refused for wait. Every call takes the limits, so they follow the settings.
//
// Like Limiter its table has a size limit and drops the key whose count started the longest ago when it is full.
type Lockout struct {
	mu      sync.Mutex
	items   map[string]*list.Element // key -> element of order, whose value is *lockItem
	order   *list.List               // oldest count start at the front
	maxKeys int
	now     func() time.Time
}

type lockItem struct {
	key   string
	start time.Time
	n     int
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
	return 0
}

// Begin is Left and, when key may try, counts the try before it is made, so that tries sent at the same time cannot
// pass the limit. The try that reaches max starts the wait; Clear it when the try succeeds. max 0 counts nothing.
func (l *Lockout) Begin(key string, max int, wait time.Duration) time.Duration {
	return l.count(key, max, wait, true)
}

// Fail counts a failure of key after it was made and gives the wait that starts with it (0 while tries are left).
func (l *Lockout) Fail(key string, max int, wait time.Duration) time.Duration {
	return l.count(key, max, wait, false)
}

func (l *Lockout) count(key string, limit int, wait time.Duration, before bool) time.Duration {
	if limit <= 0 {
		return 0
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	el, ok := l.items[key]
	if !ok {
		for len(l.items) >= l.maxKeys {
			front := l.order.Front()
			delete(l.items, front.Value.(*lockItem).key)
			l.order.Remove(front)
		}
		el = l.order.PushBack(&lockItem{key: key, start: now})
		l.items[key] = el
	}
	it := el.Value.(*lockItem)
	if left := it.until.Sub(now); left > 0 {
		return left
	}
	if !it.until.IsZero() || now.Sub(it.start) >= wait {
		// the wait or the window is over: a fresh count
		it.start, it.n, it.until = now, 0, time.Time{}
		l.order.MoveToBack(el)
	}
	it.n++
	if it.n < limit {
		return 0
	}
	it.until = now.Add(wait)
	if before {
		// the last try may still be made; the wait applies from the next one
		return 0
	}
	return wait
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
