// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package ascratelimit

import (
	"fmt"
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

func TestAFloodOfNewKeysDoesNotLockOthersOut(t *testing.T) {
	anonymous := New(2, time.Minute)
	people := New(2, time.Minute)
	at := time.Unix(1000, 0)
	anonymous.now = func() time.Time { return at }
	people.now = func() time.Time { return at }

	for i := 0; i < 10*DefaultMaxKeys; i++ {
		anonymous.Allow(fmt.Sprintf("ip-%d", i))
	}
	assert.LessOrEqual(t, anonymous.Len(), DefaultMaxKeys, "the table does not grow with the flood")
	assert.True(t, people.Allow("u7"), "a signed in person has a table of their own")
	assert.True(t, anonymous.Allow("a-new-visitor"), "a new key is served when the table is full: the oldest key makes room")
	assert.True(t, anonymous.Allow(ClientKey("[2001:db8:1:2:ffff::1]:99")), "a fresh /64 is served")

	// a key over its limit is still refused
	assert.True(t, anonymous.Allow("noisy"))
	assert.True(t, anonymous.Allow("noisy"))
	assert.False(t, anonymous.Allow("noisy"))
}

func TestClientKeyGroupsAnIPv6AddressByItsSlash64(t *testing.T) {
	assert.Equal(t, "10.0.0.1", ClientKey("10.0.0.1:5432"))
	assert.Equal(t, "10.0.0.1", ClientKey("10.0.0.1"))
	assert.Equal(t, "10.0.0.1", ClientKey("[::ffff:10.0.0.1]:80"), "an IPv4 address in IPv6 form is the IPv4 address")
	assert.Equal(t, ClientKey("[2001:db8:1:2::1]:80"), ClientKey("[2001:db8:1:2:aaaa:bbbb:cccc:dddd]:9999"), "one /64, one key")
	assert.NotEqual(t, ClientKey("[2001:db8:1:2::1]:80"), ClientKey("[2001:db8:1:3::1]:80"), "another /64, another key")
	assert.Equal(t, "plain", ClientKey("plain"))

	l := New(2, time.Minute)
	assert.True(t, l.Allow(ClientKey("[2001:db8:1:2::1]:80")))
	assert.True(t, l.Allow(ClientKey("[2001:db8:1:2::2]:81")))
	assert.False(t, l.Allow(ClientKey("[2001:db8:1:2::3]:82")), "the three addresses of one /64 share one bucket")
}
