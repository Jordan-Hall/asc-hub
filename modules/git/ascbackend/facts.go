// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package ascbackend

import (
	"container/list"
	"sync"

	"forgejo.org/modules/git"
)

// treeLoc says where a tree can be listed: below the commit rev, at path ("" for the root tree).
type treeLoc struct {
	rev  string
	path string
}

// fact is what this Backend has learned about one object id from the answers it has verified.
// Object ids are content addresses, so a fact never goes stale; it only goes missing.
type fact struct {
	typ  git.ObjectType // "blob", "tree" or "commit"
	size int64          // blobs only; -1 when unknown
	loc  *treeLoc       // trees only
}

type factEntry struct {
	key string
	f   fact
}

// factCache is a bounded LRU of facts by object id. It is what lets ReadTree (which the server
// answers by commit and path) take a bare tree id.
type factCache struct {
	mu  sync.Mutex
	max int
	ll  *list.List
	m   map[string]*list.Element
}

func newFactCache(max int) *factCache {
	return &factCache{max: max, ll: list.New(), m: map[string]*list.Element{}}
}

func (c *factCache) get(id git.ObjectID) (fact, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	el, ok := c.m[id.String()]
	if !ok {
		return fact{}, false
	}
	c.ll.MoveToFront(el)
	return el.Value.(*factEntry).f, true
}

// update merges what is known into the entry of id: a type, a size, a location.
func (c *factCache) update(id git.ObjectID, typ git.ObjectType, size int64, loc *treeLoc) {
	key := id.String()
	c.mu.Lock()
	defer c.mu.Unlock()
	if el, ok := c.m[key]; ok {
		e := el.Value.(*factEntry)
		if e.f.typ == typ || e.f.typ == "" {
			e.f.typ = typ
			if size >= 0 {
				e.f.size = size
			}
			if loc != nil && e.f.loc == nil {
				e.f.loc = loc
			}
		}
		c.ll.MoveToFront(el)
		return
	}
	c.m[key] = c.ll.PushFront(&factEntry{key: key, f: fact{typ: typ, size: size, loc: loc}})
	for c.ll.Len() > c.max {
		last := c.ll.Back()
		c.ll.Remove(last)
		delete(c.m, last.Value.(*factEntry).key)
	}
}

func (c *factCache) putCommit(id git.ObjectID)           { c.update(id, git.ObjectCommit, -1, nil) }
func (c *factCache) putBlob(id git.ObjectID, size int64) { c.update(id, git.ObjectBlob, size, nil) }
func (c *factCache) putTree(id git.ObjectID, loc treeLoc) {
	c.update(id, git.ObjectTree, -1, &loc)
}

// commitCache is a bounded LRU of decoded commits by id.
type commitCache struct {
	mu  sync.Mutex
	max int
	ll  *list.List
	m   map[string]*list.Element
}

type commitEntry struct {
	key  string
	data *git.CommitData
}

func newCommitCache(max int) *commitCache {
	return &commitCache{max: max, ll: list.New(), m: map[string]*list.Element{}}
}

// clone copies the parts of a CommitData a caller could write to.
func cloneCommit(d *git.CommitData) *git.CommitData {
	c := *d
	c.Parents = append([]git.ObjectID(nil), d.Parents...)
	if d.Author != nil {
		a := *d.Author
		c.Author = &a
	}
	if d.Committer != nil {
		a := *d.Committer
		c.Committer = &a
	}
	if d.Signature != nil {
		s := *d.Signature
		c.Signature = &s
	}
	return &c
}

func (c *commitCache) get(id git.ObjectID) (*git.CommitData, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	el, ok := c.m[id.String()]
	if !ok {
		return nil, false
	}
	c.ll.MoveToFront(el)
	return cloneCommit(el.Value.(*commitEntry).data), true
}

func (c *commitCache) put(id git.ObjectID, d *git.CommitData) {
	key := id.String()
	c.mu.Lock()
	defer c.mu.Unlock()
	if el, ok := c.m[key]; ok {
		c.ll.MoveToFront(el)
		return
	}
	c.m[key] = c.ll.PushFront(&commitEntry{key: key, data: cloneCommit(d)})
	for c.ll.Len() > c.max {
		last := c.ll.Back()
		c.ll.Remove(last)
		delete(c.m, last.Value.(*commitEntry).key)
	}
}
