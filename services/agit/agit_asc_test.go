// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package agit

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAscStackTopic(t *testing.T) {
	ch := strings.Repeat("k", 32)
	got, ok := ascChangeOfTopic("asc-" + ch)
	assert.True(t, ok)
	assert.Equal(t, ch, got)

	for _, bad := range []string{"", "asc-", "asc-" + ch[:31], "asc-" + ch + "k", "asc-" + strings.Repeat("a", 32),
		"x-" + ch, "asc-" + ch[:31] + "/", "ASC-" + ch, "asc-" + ch[:30] + "\n\n"} {
		_, ok := ascChangeOfTopic(bad)
		assert.False(t, ok, "%q", bad)
	}
}

func TestAscStackChangeCodec(t *testing.T) {
	ch := "kzlymxnwovpuqtrs" + "kzlymxnwovpuqtrs"
	b, err := ascChangeBytes(ch)
	require.NoError(t, err)
	require.Len(t, b, 16)
	// 'z' is digit 0 and 'k' is digit 15.
	assert.Equal(t, byte(0xf0), b[0])
	back, err := ascChangeText(b)
	require.NoError(t, err)
	assert.Equal(t, ch, back)
	_, err = ascChangeBytes("short")
	require.Error(t, err)
	_, err = ascChangeText(make([]byte, 15))
	require.Error(t, err)
}

// A user without write access, or a non-author without "allow edits", cannot update a PR by forging preds.
func TestAscStackUpdateAuthorisation(t *testing.T) {
	cases := []struct {
		name                                    string
		author, adminOrOwner, write, allowEdits bool
		want                                    bool
	}{
		{"author", true, false, false, false, true},
		{"admin or owner", false, true, false, false, true},
		{"writer with allow edits", false, false, true, true, true},
		{"writer without allow edits", false, false, true, false, false},
		{"no write access with allow edits", false, false, false, true, false},
		{"stranger", false, false, false, false, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assert.Equal(t, c.want, ascUpdateAllowed(c.author, c.adminOrOwner, c.write, c.allowEdits))
		})
	}
	assert.Equal(t, "not allowed to update change kkkkkkkkkkkk", ascDeniedMessage(strings.Repeat("k", 32)))
}
