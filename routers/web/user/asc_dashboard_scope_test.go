// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package user

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDashboardScopeKeepsOnlyWhatIsInIt(t *testing.T) {
	assert.Equal(t, "acme", ascOwnerOf("acme/payments"))
	assert.Equal(t, "acme", ascOwnerOf("/acme/payments/pulls/3"))
	assert.Empty(t, ascOwnerOf(""))

	for _, c := range []struct {
		scope, owner string
		want         bool
	}{
		{"", "acme", true},
		{"all", "acme", true},
		{"personal", "Jordan", true},
		{"personal", "acme", false},
		{"org:acme", "ACME", true},
		{"org:acme", "jordan", false},
		{"org:acme", "acme-labs", false},
		{"nonsense", "acme", true}, // an unknown scope is Everything, never a way to hide or show something else
	} {
		assert.Equal(t, c.want, ascScopeKeep(c.scope, "jordan", c.owner), "%+v", c)
	}
}
