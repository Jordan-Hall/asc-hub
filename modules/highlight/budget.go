// Copyright 2026 The ASC Hub Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package highlight

import (
	"container/list"
	"crypto/sha256"
	"encoding/hex"
	"html/template"
	"strings"
	"sync"
	"time"

	hubcache "forgejo.org/modules/cache"

	"github.com/alecthomas/chroma/v2"
)

// Highlighting is the slowest step of showing a file (about 1 to 3 milliseconds per KB with the chroma lexers), so
// File is bounded and cached:
//
//   - a file larger than highlightMaxBytes, or one whose tokens are not ready after highlightBudget, is shown as
//     plain text and the caller says so (Result.Skipped), so the page never waits on one big file;
//   - the highlighted lines are kept by (highlighter version, lexer, content hash) in a memory LRU bounded by
//     bytes and, for the files worth it, in the hub's cache (modules/cache: memory, Redis or the configured
//     adapter), so a second view, another person or another page (blame, review, search) costs a lookup.
//
// highlighterVersion changes whenever a lexer or the output format does; it is part of every key.
const (
	highlighterVersion = "hl2"

	// highlightMaxBytes is the largest file that is highlighted at all.
	highlightMaxBytes = 256 * 1024
	// highlightBudget is the time one file may take to tokenize.
	highlightBudget = 1500 * time.Millisecond

	// memoryCacheBytes bounds the HTML kept in the process; the unit is the size of the highlighted lines.
	memoryCacheBytes = 64 << 20
	// persistentMinBytes is the smallest file written to the hub's cache: below it highlighting is cheaper than the trip.
	persistentMinBytes = 8 << 10
	persistentTTL      = 24 * 60 * 60 // seconds
	lineSeparator      = "\x00"
)

// Result is a highlighted file.
type Result struct {
	Lines []template.HTML
	// Lexer is the display name of the lexer, "" when the file was not highlighted.
	Lexer string
	// Skipped is true when the file is plain text because it is too large or took too long to highlight.
	Skipped bool
	// Cached is true when the lines came from a cache.
	Cached bool
}

type memEntry struct {
	key   string
	lines []template.HTML
	lexer string
	size  int
}

// byteLRU is an LRU of highlighted files bounded by the bytes of their lines.
type byteLRU struct {
	mu    sync.Mutex
	max   int
	size  int
	order *list.List
	items map[string]*list.Element
}

func newByteLRU(max int) *byteLRU {
	return &byteLRU{max: max, order: list.New(), items: map[string]*list.Element{}}
}

func (c *byteLRU) get(key string) (*memEntry, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	el, ok := c.items[key]
	if !ok {
		return nil, false
	}
	c.order.MoveToFront(el)
	return el.Value.(*memEntry), true
}

func (c *byteLRU) put(key string, lines []template.HTML, lexer string) {
	size := len(key) + len(lexer)
	for _, l := range lines {
		size += len(l)
	}
	if size > c.max/4 { // one file never takes a quarter of the cache
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if el, ok := c.items[key]; ok {
		c.size -= el.Value.(*memEntry).size
		c.order.Remove(el)
		delete(c.items, key)
	}
	c.items[key] = c.order.PushFront(&memEntry{key: key, lines: lines, lexer: lexer, size: size})
	c.size += size
	for c.size > c.max {
		last := c.order.Back()
		if last == nil {
			break
		}
		e := last.Value.(*memEntry)
		c.size -= e.size
		delete(c.items, e.key)
		c.order.Remove(last)
	}
}

var resultCache = newByteLRU(memoryCacheBytes)

// cacheKey identifies a highlighted file: the version of the highlighter, the lexer and the content.
func cacheKey(lexerName string, code []byte) string {
	h := sha256.New()
	h.Write([]byte(highlighterVersion))
	h.Write([]byte{0})
	h.Write([]byte(lexerName))
	h.Write([]byte{0})
	h.Write(code)
	return "highlight:" + highlighterVersion + ":" + hex.EncodeToString(h.Sum(nil))
}

// persistentGet and persistentPut talk to the hub's cache. They are variables so that a test can replace them.
var (
	persistentGet = func(key string) (string, bool) {
		c := hubcache.GetCache()
		if c == nil {
			return "", false
		}
		v, ok := c.Get(key).(string)
		return v, ok
	}
	persistentPut = func(key, value string) {
		if c := hubcache.GetCache(); c != nil {
			_ = c.Put(key, value, persistentTTL)
		}
	}
)

func encodeLines(lexer string, lines []template.HTML) string {
	var sb strings.Builder
	sb.WriteString(lexer)
	for _, l := range lines {
		sb.WriteString(lineSeparator)
		sb.WriteString(string(l))
	}
	return sb.String()
}

func decodeLines(s string) (lexer string, lines []template.HTML, ok bool) {
	parts := strings.Split(s, lineSeparator)
	if len(parts) < 1 {
		return "", nil, false
	}
	lines = make([]template.HTML, 0, len(parts)-1)
	for _, p := range parts[1:] {
		lines = append(lines, template.HTML(p))
	}
	return parts[0], lines, true
}

// Warm highlights code the way File does and keeps the result in the caches, and says whether it did. A file that is
// too large, too slow or already cached is left as it is; the caller is the push queue and has no use for the lines.
func Warm(fileName string, code []byte) bool {
	res, err := FileBudgeted(fileName, "", code)
	return err == nil && !res.Skipped && !res.Cached
}

// tokensWithin reads the tokens of an iterator until it ends or the deadline passes. The clock is read every 512
// tokens, so the check costs nothing and a slow file stops within a few milliseconds of the deadline.
func tokensWithin(it chroma.Iterator, deadline time.Time) ([]chroma.Token, bool) {
	var out []chroma.Token
	for tok := it(); tok != chroma.EOF; tok = it() {
		out = append(out, tok)
		if len(out)&511 == 0 && time.Now().After(deadline) {
			return nil, false
		}
	}
	return out, true
}
