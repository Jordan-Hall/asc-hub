// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

// Package ascratelimit is a small in-memory limiter for the endpoints that answer on every keystroke (the command
// palette, the user name check of the sign up). It counts the calls of one key in a window: at most Max calls per
// Window for each key. It keeps no state across restarts.
//
// The table has a size limit. When it is full the key that has been in it the longest is dropped, so a flood of new
// keys (many addresses, many ports) can neither grow the memory nor lock a new key out: the worst it does is to give
// the keys it pushes out a fresh count. A key is only ever refused for its own calls.
package ascratelimit

import (
	"container/list"
	"net"
	"sync"
	"time"
)

// Limiter allows at most Max calls per Window for each key. Use one limiter for one kind of caller (people who are
// signed in, and visitors who are not, do not share a table: a flood of visitors cannot push a person out).
type Limiter struct {
	Max    int
	Window time.Duration

	mu      sync.Mutex
	hits    map[string]*list.Element // key -> element of order, whose value is *bucket
	order   *list.List               // oldest window start at the front
	maxKeys int
	now     func() time.Time
}

type bucket struct {
	key   string
	start time.Time
	n     int
}

// DefaultMaxKeys bounds the memory of a limiter.
const DefaultMaxKeys = 10000

// New makes a limiter.
func New(max int, window time.Duration) *Limiter {
	return &Limiter{Max: max, Window: window, hits: map[string]*list.Element{}, order: list.New(), maxKeys: DefaultMaxKeys, now: time.Now}
}

// Allow counts one call of key and reports whether it is within the limit.
func (l *Limiter) Allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	if el, ok := l.hits[key]; ok {
		b := el.Value.(*bucket)
		if now.Sub(b.start) >= l.Window {
			b.start, b.n = now, 1
			l.order.MoveToBack(el)
			return true
		}
		b.n++
		return b.n <= l.Max
	}
	for len(l.hits) >= l.maxKeys {
		front := l.order.Front()
		delete(l.hits, front.Value.(*bucket).key)
		l.order.Remove(front)
	}
	l.hits[key] = l.order.PushBack(&bucket{key: key, start: now, n: 1})
	return true
}

// Len is the number of keys in the table.
func (l *Limiter) Len() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.hits)
}

// ClientKey returns the key of a client from the remote address of a request: the host without the port, and for an
// IPv6 address its /64 (one customer holds a whole /64, so a flood from one customer is one key, not 2^64).
func ClientKey(remoteAddr string) string {
	host := remoteAddr
	if h, _, err := net.SplitHostPort(remoteAddr); err == nil {
		host = h
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return host
	}
	if v4 := ip.To4(); v4 != nil {
		return v4.String()
	}
	return ip.Mask(net.CIDRMask(64, 128)).String() + "/64"
}
