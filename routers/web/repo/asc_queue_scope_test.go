// Copyright 2026 The ASC Hub Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	"testing"

	pull_service "forgejo.org/services/pull"

	"github.com/stretchr/testify/assert"
)

func TestAscLandScopes(t *testing.T) {
	panel := &pull_service.AscStackPanel{Entries: []pull_service.AscStackEntry{
		{Index: 5, Title: "one", State: "open", Position: 1},
		{Index: 6, Title: "two", State: "open", Position: 2, Current: true},
		{Index: 7, Title: "three", State: "open", Position: 3},
		{Index: 4, Title: "landed", State: "merged", Position: 0},
	}}
	scopes := ascLandScopes(panel, 6)
	assert.Len(t, scopes, 3, "a landed change is not a choice")
	assert.Equal(t, 1, scopes[0].Count)
	assert.True(t, scopes[1].Checked)
	assert.False(t, scopes[0].Checked)
	assert.Equal(t, 3, scopes[2].Total)
	assert.Nil(t, ascLandScopes(nil, 1))

	assert.True(t, ascStackHasIndex(panel, 7))
	assert.False(t, ascStackHasIndex(panel, 99), "a number outside the stack is refused")
	assert.False(t, ascStackHasIndex(nil, 7))
}
