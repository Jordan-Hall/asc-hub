// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package ascclient

import (
	"container/list"
	"sync"
	"sync/atomic"

	ascv1 "forgejo.org/modules/ascproto/ascv1"
)

// cacheKey is an object kind and its 32-byte id. Objects are immutable and content-addressed,
// so an entry never goes stale and needs no invalidation. The key also names the principal and
// the repository the object was authorised for: a hit never answers a caller whose own
// credential did not fetch the object from that repository (SPEC section 15, authorise every
// RPC). Without that, a cache hit would let any user read an object by guessing its id.
type cacheKey struct {
	scope [16]byte
	repo  string
	kind  ascv1.ObjKind
	id    [32]byte
}

type cacheEntry struct {
	key  cacheKey
	data []byte
}

// CacheStats counts cache traffic.
type CacheStats struct {
	Hits, Misses uint64
	Entries      int
	Bytes        int64
}

// objectCache is a byte-bounded LRU of decoded, hash-verified objects. A value is shared with
// every reader, so callers must treat the bytes as read-only.
type objectCache struct {
	mu       sync.Mutex
	max      int64
	used     int64
	order    *list.List // front = most recently used
	items    map[cacheKey]*list.Element
	hits     atomic.Uint64
	misses   atomic.Uint64
	maxEntry int64
}

func newObjectCache(maxBytes int64) *objectCache {
	if maxBytes <= 0 {
		return nil
	}
	return &objectCache{
		max:      maxBytes,
		order:    list.New(),
		items:    make(map[cacheKey]*list.Element),
		maxEntry: maxBytes / 4, // one huge object must not flush the rest
	}
}

func (c *objectCache) get(k cacheKey) ([]byte, bool) {
	if c == nil {
		return nil, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	el, ok := c.items[k]
	if !ok {
		c.misses.Add(1)
		return nil, false
	}
	c.order.MoveToFront(el)
	c.hits.Add(1)
	return el.Value.(*cacheEntry).data, true
}

func (c *objectCache) put(k cacheKey, data []byte) {
	if c == nil {
		return
	}
	size := entrySize(k, data)
	if size > c.maxEntry {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if el, ok := c.items[k]; ok {
		c.order.MoveToFront(el)
		return
	}
	c.items[k] = c.order.PushFront(&cacheEntry{key: k, data: data})
	c.used += size
	for c.used > c.max {
		last := c.order.Back()
		e := last.Value.(*cacheEntry)
		c.order.Remove(last)
		delete(c.items, e.key)
		c.used -= entrySize(e.key, e.data)
	}
}

func (c *objectCache) stats() CacheStats {
	if c == nil {
		return CacheStats{}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return CacheStats{Hits: c.hits.Load(), Misses: c.misses.Load(), Entries: len(c.items), Bytes: c.used}
}

// entrySize charges the data plus a fixed allowance for the key, list element and map slot.
func entrySize(k cacheKey, data []byte) int64 {
	return int64(len(data)) + int64(len(k.repo)) + 128
}
