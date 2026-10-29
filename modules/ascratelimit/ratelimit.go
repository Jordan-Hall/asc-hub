// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

// Package ascratelimit is a small in-memory limiter for the endpoints that answer on every keystroke (the command
// palette, the user name check of the sign up). It counts the calls of one key in a window that slides in steps of
// one window: at most Max calls per Window for each key. It keeps no state across restarts.
package ascratelimit

import (
	"net"
	"sync"
	"time"
)

// Limiter allows at most Max calls per Window for each key.
type Limiter struct {
	Max    int
	Window time.Duration

	mu   sync.Mutex
	hits map[string]*bucket
	now  func() time.Time
}

type bucket struct {
	start time.Time
	n     int
}

// maxKeys bounds the memory: when it is reached, the buckets of finished windows go.
const maxKeys = 10000

// New makes a limiter.
func New(max int, window time.Duration) *Limiter {
	return &Limiter{Max: max, Window: window, hits: map[string]*bucket{}, now: time.Now}
}

// Allow counts one call of key and reports whether it is within the limit.
func (l *Limiter) Allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	if len(l.hits) >= maxKeys {
		for k, b := range l.hits {
			if now.Sub(b.start) >= l.Window {
				delete(l.hits, k)
			}
		}
		if len(l.hits) >= maxKeys {
			// all keys are busy: refuse the new ones rather than grow
			if _, ok := l.hits[key]; !ok {
				return false
			}
		}
	}
	b := l.hits[key]
	if b == nil || now.Sub(b.start) >= l.Window {
		l.hits[key] = &bucket{start: now, n: 1}
		return true
	}
	b.n++
	return b.n <= l.Max
}

// ClientKey returns the key of a client from the remote address of a request: the host without the port.
func ClientKey(remoteAddr string) string {
	if host, _, err := net.SplitHostPort(remoteAddr); err == nil {
		return host
	}
	return remoteAddr
}
