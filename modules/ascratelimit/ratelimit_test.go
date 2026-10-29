// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package ascratelimit

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestAllowCountsPerKeyAndWindow(t *testing.T) {
	l := New(3, time.Minute)
	at := time.Unix(1000, 0)
	l.now = func() time.Time { return at }
	for i := 0; i < 3; i++ {
		assert.True(t, l.Allow("a"), "call %d", i)
	}
	assert.False(t, l.Allow("a"), "the fourth call is over the limit")
	assert.True(t, l.Allow("b"), "another key has its own count")
	at = at.Add(time.Minute)
	assert.True(t, l.Allow("a"), "a new window starts")
}

func TestAllowDoesNotGrowWithoutBound(t *testing.T) {
	l := New(1, time.Hour)
	for i := 0; i < maxKeys; i++ {
		l.hits[string(rune('a'+i%26))+time.Duration(i).String()] = &bucket{start: l.now(), n: 1}
	}
	assert.False(t, l.Allow("new-key"), "a new key is refused while all keys are busy")
}

func TestClientKeyDropsThePort(t *testing.T) {
	assert.Equal(t, "10.0.0.1", ClientKey("10.0.0.1:5432"))
	assert.Equal(t, "::1", ClientKey("[::1]:80"))
	assert.Equal(t, "plain", ClientKey("plain"))
}
