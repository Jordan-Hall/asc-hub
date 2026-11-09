// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package user

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAscKnowsDir(t *testing.T) {
	for path, want := range map[string]string{
		"README.md":                   "/",
		"tests/a_test.go":             "tests/",
		"src/payments/retry.go":       "src/payments/",
		"src/payments/ledger/post.go": "src/payments/",
		"/src/x.go":                   "src/",
	} {
		assert.Equal(t, want, ascKnowsDir(path), path)
	}
	assert.Equal(t, map[string]int{"src/": 2, "/": 1}, ascKnowsDirs([]string{"src/a.go", "src/b.go", "go.mod", ""}))
}

func TestAscKnowsRank(t *testing.T) {
	assert.Nil(t, ascKnowsRank(nil, map[string]int{}))

	rows := ascKnowsRank(
		map[string]int{"src/payments/": 40, "tests/": 10, "docs/": 5},
		map[string]int{"src/refunds/": 30, "src/payments/": 6, "build/": 9},
	)
	require.Len(t, rows, ascKnowsMax)
	assert.Equal(t, AscKnows{Path: "src/payments/", Share: 46, Bar: 45, Changes: 40, Reviews: 6}, rows[0])
	assert.Equal(t, AscKnows{Path: "src/refunds/", Share: 30, Bar: 30, Reviews: 30}, rows[1])
	assert.Equal(t, "tests/", rows[2].Path, "10 files before build/ with 9")

	tie := ascKnowsRank(map[string]int{"b/": 1, "a/": 1}, nil)
	assert.Equal(t, "a/", tie[0].Path, "a tie goes by path")
	assert.Equal(t, 50, tie[0].Share)
}
