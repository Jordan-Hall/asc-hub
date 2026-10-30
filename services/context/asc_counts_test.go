// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package context

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAscCountGates(t *testing.T) {
	type in struct{ signed, code, actions, features bool }
	for name, tc := range map[string]struct {
		in         in
		runs, deps bool
	}{
		"anonymous on a public repository":   {in{false, true, true, true}, false, false},
		"signed in, reads no code":           {in{true, false, true, true}, false, false},
		"reads the code only":                {in{true, true, false, false}, false, true},
		"reads code and actions, no feature": {in{true, true, true, false}, false, true},
		"reads code and actions, feature on": {in{true, true, true, true}, true, true},
		"reads actions but not the code":     {in{true, false, true, true}, false, false},
	} {
		runs, deps := ascCountGates(tc.in.signed, tc.in.code, tc.in.actions, tc.in.features)
		assert.Equal(t, tc.runs, runs, name+": agent runs")
		assert.Equal(t, tc.deps, deps, name+": dependencies")
	}
}
