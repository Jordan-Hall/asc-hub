// Copyright 2026 The ASC Hub Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package repo

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseBehind(t *testing.T) {
	n, ok := parseBehind("4\n")
	assert.True(t, ok)
	assert.Equal(t, 4, n)
	n, ok = parseBehind("0")
	assert.True(t, ok)
	assert.Zero(t, n)
	for _, bad := range []string{"", "x", "-1", "1 2"} {
		_, ok := parseBehind(bad)
		assert.False(t, ok, bad)
	}
}
