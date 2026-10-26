// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseAscVersionNumber(t *testing.T) {
	for in, want := range map[string]uint32{"1": 1, "7": 7, "4294967295": 4294967295, "0012": 12} {
		n, ok := parseAscVersionNumber(in)
		assert.True(t, ok, in)
		assert.Equal(t, want, n, in)
	}
	for _, in := range []string{"", "0", "-1", "+1", " 1", "1 ", "1.0", "0x10", "4294967296", "99999999999", "١٢", "1;2"} {
		_, ok := parseAscVersionNumber(in)
		assert.False(t, ok, in)
	}
}
