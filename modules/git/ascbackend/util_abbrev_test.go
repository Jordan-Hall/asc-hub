// Copyright 2026 The ASC Hub Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package ascbackend

import "testing"

func TestIsShortHexID(t *testing.T) {
	for s, want := range map[string]bool{
		"685806f":                                  true,
		"abcd":                                     true,
		"abc":                                      false,
		"":                                         false,
		"main":                                     false,
		"685806f55d8394b96bde6da4a2b93b467047ed70": false,
		"685806fg":                                 false,
		"ABCDEF1":                                  true,
	} {
		if got := isShortHexID(s); got != want {
			t.Errorf("isShortHexID(%q) = %v, want %v", s, got, want)
		}
	}
}
