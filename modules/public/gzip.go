// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package public

import (
	"bytes"
	"io"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/klauspost/compress/gzip"
)

// Static assets are text the hub ships (scripts, styles, icons) and hold nothing about the person, so compressing
// them carries none of the risk of compressing pages. A build that embeds the assets serves them zstd-compressed to
// a browser that takes zstd; every other case (assets on disk, a browser without zstd) would send them raw, which on
// a slow link is most of the wait for a page. Those are gzip-compressed once and kept in memory.

const (
	// gzipMaxFile is the largest asset that is compressed; a larger one is sent as it is.
	gzipMaxFile = 16 << 20
	// gzipMaxCache bounds the compressed bytes kept; once full, further assets are sent as they are.
	gzipMaxCache = 64 << 20
)

// compressibleExt are the extensions of assets that compress well. Images other than SVG and web fonts are
// compressed already.
var compressibleExt = map[string]bool{
	".js": true, ".mjs": true, ".css": true, ".map": true, ".svg": true, ".json": true, ".txt": true,
	".html": true, ".xml": true, ".wasm": true, ".ttf": true, ".otf": true, ".eot": true, ".ico": true,
}

// compressible reports whether the asset name is worth compressing.
func compressible(name string) bool {
	return compressibleExt[strings.ToLower(path.Ext(name))]
}

type gzipKey struct {
	name    string
	modtime int64
	size    int64
}

// gzipCache holds the gzip form of assets, keyed by name, time and size, so a changed file is compressed anew.
type gzipCache struct {
	mu    sync.Mutex
	items map[gzipKey][]byte
	bytes int
}

var assetGzip = &gzipCache{items: map[gzipKey][]byte{}}

// get returns the gzip form of content, compressing it on first use. It returns nil, and leaves content where it
// was, when the asset is too large, the cache is full or the asset does not shrink.
func (c *gzipCache) get(name string, modtime time.Time, content io.ReadSeeker) []byte {
	size, err := content.Seek(0, io.SeekEnd)
	if err != nil || size <= 0 || size > gzipMaxFile {
		_, _ = content.Seek(0, io.SeekStart)
		return nil
	}
	if _, err := content.Seek(0, io.SeekStart); err != nil {
		return nil
	}
	key := gzipKey{name: name, modtime: modtime.UnixNano(), size: size}
	c.mu.Lock()
	z, ok := c.items[key]
	full := c.bytes >= gzipMaxCache
	c.mu.Unlock()
	if ok {
		return z
	}
	if full {
		return nil
	}
	raw, err := io.ReadAll(io.LimitReader(content, gzipMaxFile+1))
	_, _ = content.Seek(0, io.SeekStart)
	if err != nil || int64(len(raw)) != size {
		return nil
	}
	var buf bytes.Buffer
	w, err := gzip.NewWriterLevel(&buf, gzip.BestCompression)
	if err != nil {
		return nil
	}
	if _, err := w.Write(raw); err != nil {
		return nil
	}
	if err := w.Close(); err != nil {
		return nil
	}
	if buf.Len() >= len(raw) {
		return nil
	}
	z = buf.Bytes()
	c.mu.Lock()
	defer c.mu.Unlock()
	if prev, ok := c.items[key]; ok {
		return prev
	}
	if c.bytes+len(z) > gzipMaxCache {
		return z // served this once, not kept
	}
	c.items[key] = z
	c.bytes += len(z)
	return z
}
