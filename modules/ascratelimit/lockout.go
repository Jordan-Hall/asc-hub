// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package ascratelimit

import (
	"container/list"
	"sync"
	"time"
)

// Lockout counts the tries of a key, for the wait after wrong passwords: once limit tries were counted within wait of
// the first, the key is refused for wait. Every call takes the limits, so they follow the settings.
//
// Its table has a size limit. When it is full it drops the key whose count started the longest ago among those not
// waiting. When every key waits it drops the one whose wait ends soonest: each key keeps its own count, so a flood
// never refuses or frees keys it did not count, and it only shortens the waits that were nearly over.
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
	limit int
	until time.Time
}

// Ticket is a try counted by Begin, for Release. The zero Ticket is a try that was not counted.
type Ticket struct {
	it    *lockItem
	start time.Time
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
// pass the limit. The try that reaches limit starts the wait; Clear the key, or Release the ticket, when the try
// turns out not to count. limit 0 counts nothing.
func (l *Lockout) Begin(key string, limit int, wait time.Duration) (time.Duration, Ticket) {
	if limit <= 0 {
		return 0, Ticket{}
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	el, ok := l.items[key]
	if !ok {
		if len(l.items) >= l.maxKeys {
			l.dropOne(now)
		}
		el = l.order.PushBack(&lockItem{key: key, start: now})
		l.items[key] = el
	}
	it := el.Value.(*lockItem)
	if left := it.until.Sub(now); left > 0 {
		return left, Ticket{}
	}
	if !it.until.IsZero() || now.Sub(it.start) >= wait {
		// the wait or the window is over: a fresh count
		it.start, it.n, it.until = now, 0, time.Time{}
		l.order.MoveToBack(el)
	}
	it.n++
	it.limit = limit
	if it.n >= limit {
		// the last try may still be made; the wait applies from the next one
		it.until = now.Add(wait)
	}
	return 0, Ticket{it: it, start: it.start}
}

// dropOne drops the key whose count started the longest ago among those not waiting, else the one whose wait ends
// soonest.
func (l *Lockout) dropOne(now time.Time) {
	var soonest *list.Element
	for el := l.order.Front(); el != nil; el = el.Next() {
		it := el.Value.(*lockItem)
		if !it.until.After(now) {
			soonest = el
			break
		}
		if soonest == nil || it.until.Before(soonest.Value.(*lockItem).until) {
			soonest = el
		}
	}
	if soonest != nil {
		delete(l.items, soonest.Value.(*lockItem).key)
		l.order.Remove(soonest)
	}
}

// Release gives back the try of t, which turned out not to count (it succeeded, or the server failed). When that try
// started the wait, the wait ends again. A try whose key was dropped or whose count started again is not given back.
func (l *Lockout) Release(t Ticket) {
	if t.it == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	el, ok := l.items[t.it.key]
	if !ok || el.Value.(*lockItem) != t.it || !t.it.start.Equal(t.start) {
		return
	}
	t.it.n = max(t.it.n-1, 0)
	if t.it.n < t.it.limit {
		t.it.until = time.Time{}
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
