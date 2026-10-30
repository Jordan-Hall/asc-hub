// Copyright 2026 The ASC Hub Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package repository

import (
	"fmt"
	"testing"
	"time"

	"forgejo.org/modules/highlight"

	"github.com/stretchr/testify/assert"
)

func TestSelectWarmPaths(t *testing.T) {
	got := selectWarmPaths([]string{"a.go", "", "a.go", "web/app.min.js", "Cargo.lock", "b.rs", "c.ts"}, 3)
	assert.Equal(t, []string{"a.go", "b.rs", "c.ts"}, got, "no empty, repeated, minified or lock files, at most max")
	assert.Empty(t, selectWarmPaths(nil, 5))
}

func TestWarmFillsTheHighlightCache(t *testing.T) {
	// The highlight cache is one for the process: the source differs on every run (go test -count), or the second run
	// would find the first one's lines cached and the first Warm would have nothing to do.
	src := []byte(fmt.Sprintf("package main\n\nfunc main() {} // run %d\n", time.Now().UnixNano()))
	assert.True(t, highlight.Warm("warm_test_main.go", src), "the first highlight is stored")
	assert.False(t, highlight.Warm("warm_test_main.go", src), "a second one finds it cached")
	res, err := highlight.FileBudgeted("warm_test_main.go", "", src)
	assert.NoError(t, err)
	assert.True(t, res.Cached, "the view after a push finds the lines ready")
}
